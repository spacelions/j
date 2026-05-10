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

// verifyResumePlanFakeUI scripts clitasks.UI for the resume-plan
// e2e test below. PickTask returns the seeded id so RunResumePlan
// proceeds straight into the FSM gate.
type verifyResumePlanFakeUI struct {
	pickReturn string
}

func (u *verifyResumePlanFakeUI) ConfirmDiscard(
	context.Context, tasks.Task,
) (bool, error) {
	return false, nil
}

func (u *verifyResumePlanFakeUI) PickTask(
	_ context.Context, _ []tasks.Task,
) (string, bool, error) {
	if u.pickReturn == "" {
		return "", false, nil
	}
	return u.pickReturn, true, nil
}

// verifyArgvJStub writes a tiny shell script that records its argv
// to outputPath, one argument per line. The script first writes
// to a `.tmp` sibling and renames atomically so a polling reader
// never sees a partial argv list.
func verifyArgvJStub(t *testing.T, outputPath string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "j-verify-stub.sh")
	body := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q.tmp && mv %q.tmp %q\n",
		outputPath, outputPath, outputPath,
	)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// verifyReadStubArgv polls outputPath for the argv list written by
// verifyArgvJStub and returns the parsed argv.
func verifyReadStubArgv(t *testing.T, outputPath string) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(outputPath)
		if err == nil && len(data) > 0 {
			return strings.Split(strings.TrimSpace(string(data)), "\n")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("argv stub never wrote to %s", outputPath)
	return nil
}

// TestVerify_ResumePlan_SucceedsFromPlanPendingApproval pins the
// black-box acceptance criterion: `j tasks resume-plan` selecting a
// row in status `plan-pending-approval` (with a non-empty
// PlanResumeSession) must NOT print
// `J: cannot resume-plan task in status "plan-pending-approval"`;
// it must re-exec the orchestrator with
// `tasks orchestrate --id <id> --phase=plan-only
// --interactive=true`.
func TestVerify_ResumePlan_SucceedsFromPlanPendingApproval(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	for _, bucket := range []string{
		store.BucketPlanner,
		store.BucketWorker,
		store.BucketVerifier,
	} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}

	id := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(taskDir, tasks.RequirementsFileName),
		[]byte("requirements"),
		0o644,
	); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:                id,
		Status:            tasks.StatusPlanPendingApproval,
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		PlanResumeSession: "active-cursor",
		Summary:           "verify resume-plan from plan-pending-approval",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &verifyResumePlanFakeUI{pickReturn: id}
	agent := testutil.NewScriptedAgent()
	if err := clitasks.RunResumePlan(
		t.Context(),
		clitasks.ResumePlanOptions{
			Stdin:   strings.NewReader(""),
			Stdout:  io.Discard,
			Stderr:  io.Discard,
			Agents:  []codingagents.Agent{agent},
			UI:      ui,
			JBinary: verifyArgvJStub(t, argvPath),
		},
	); err != nil {
		t.Fatalf(
			"RunResumePlan from plan-pending-approval: %v; "+
				"the FSM gate must permit EventPlanResume",
			err,
		)
	}

	got := verifyReadStubArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--phase=plan-only", "--interactive=true",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}
