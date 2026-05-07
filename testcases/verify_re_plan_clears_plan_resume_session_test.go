package testcases_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// rePlanArgvJStub writes a tiny shell script that records its argv
// (one argument per line, atomic rename so the polling reader never
// sees a partial list). The re-plan flow re-execs the j binary as
// `j tasks orchestrate ...`; this stub stands in so the test does
// not require a built j on PATH.
func rePlanArgvJStub(t *testing.T, outputPath string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "j-stub-replan.sh")
	body := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q.tmp && mv %q.tmp %q\n",
		outputPath, outputPath, outputPath,
	)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func rePlanReadArgv(t *testing.T, path string) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return strings.Split(strings.TrimSpace(string(data)), "\n")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("argv stub never wrote to %s", path)
	return nil
}

// TestVerify_RePlan_ClearsPlanResumeSession is the black-box pin for
// the re-plan side of SPA-33. The fix uses row state as the resume
// signal: the orchestrator's planner phase reads
// PlanResumeSession and treats a non-empty value as "resume the
// prior session" and an empty value as "start fresh". For that
// inference to keep `j tasks re-plan` in the fresh-mint branch (the
// re-plan UX promise — "re-plan means start over, not continue"),
// re-plan must blank PlanResumeSession on the row before re-execing
// `j tasks orchestrate`. This test seeds a plan-done row carrying a
// stale session, drives RunRePlan with a stub j binary so the
// re-exec is captured but the orchestrator never actually fires,
// and asserts the row's PlanResumeSession is empty afterwards.
func TestVerify_RePlan_ClearsPlanResumeSession(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	for _, b := range []string{
		store.BucketPlanner, store.BucketWorker, store.BucketVerifier,
	} {
		testutil.SeedAgentBucketToolModel(t, b, "cursor", "sonnet-4")
	}
	id := tasks.NewTaskID()
	if _, err := tasks.EnsureDir(id); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	begin := time.Now().UTC().Add(-time.Hour)
	testutil.SeedTaskRow(t, tasks.Task{
		ID:                id,
		Status:            tasks.StatusPlanDone,
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		PlanResumeSession: "stale-cursor",
		Summary:           "spa-33 re-plan",
		PlanBeginAt:       begin,
		PlanEndAt:         begin.Add(time.Minute),
	})

	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	if err := clitasks.RunRePlan(
		context.Background(),
		clitasks.RePlanOptions{
			FromTask: id,
			Stdin:    strings.NewReader(""),
			Stdout:   io.Discard,
			Stderr:   io.Discard,
			Agents:   []codingagents.Agent{testutil.NewScriptedAgent()},
			JBinary:  rePlanArgvJStub(t, argvPath),
		},
	); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	args := rePlanReadArgv(t, argvPath)
	if len(args) < 4 || args[0] != "tasks" || args[1] != "orchestrate" {
		t.Fatalf(
			"argv = %v, want a `tasks orchestrate ...` re-exec",
			args,
		)
	}
	row := testutil.ReadTaskRow(t, id)
	if row.PlanResumeSession != "" {
		t.Fatalf(
			"PlanResumeSession = %q, want empty after re-plan; "+
				"the orchestrator's resume inference reads "+
				"this field and a stale value would force the "+
				"resume branch on what should be a fresh re-plan",
			row.PlanResumeSession,
		)
	}
}
