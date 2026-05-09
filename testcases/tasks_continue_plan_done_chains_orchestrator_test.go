package testcases_test

import (
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

// argvJStub writes a tiny shell script that records its argv, one
// argument per line, atomically renaming a sibling temp file into
// place so the polling reader never sees a partial argv list.
func argvJStub(t *testing.T, outputPath string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "j-argv-stub.sh")
	body := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q.tmp && mv %q.tmp %q\n",
		outputPath, outputPath, outputPath,
	)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// readArgv polls path until the stub has written its argv.
func readArgv(t *testing.T, path string) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return strings.Split(strings.TrimSpace(string(data)), "\n")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("spawned argv was not written to %s", path)
	return nil
}

// TestTasksContinue_PlanDoneChainsToOrchestratorFromWork pins the bug
// fix from SPA-30: `j tasks continue` on a `plan-done` row dispatches
// through the orchestrator with `--phase=from-work`, which is the
// surface that runs worker -> verifier sequentially. The legacy bug
// short-circuited into an in-process worker shell-out that returned
// before the verifier could fire.
func TestTasksContinue_PlanDoneChainsToOrchestratorFromWork(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	for _, b := range []string{
		store.BucketPlanner, store.BucketWorker, store.BucketVerifier,
	} {
		testutil.SeedAgentBucketToolModel(t, b, "cursor", "sonnet-4")
	}
	id := tasks.NewTaskID()
	dir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, tasks.RequirementsFileName),
		[]byte("# req\nbody"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, tasks.PlanFileName),
		[]byte("1. step\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	begin := time.Now().UTC().Add(-2 * time.Hour)
	testutil.SeedTaskRow(t, tasks.Task{
		ID:                id,
		Status:            tasks.StatusPlanDone,
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		WorkTool:          "cursor",
		WorkModel:         "sonnet-4",
		VerifyTool:        "cursor",
		VerifyModel:       "sonnet-4",
		PlanResumeSession: "plan-cursor",
		Summary:           "spa-30 fix",
		PlanBeginAt:       begin,
		PlanEndAt:         begin.Add(time.Hour),
	})

	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	jbin := argvJStub(t, argvPath)
	if err := clitasks.RunContinue(
		t.Context(),
		clitasks.ContinueOptions{
			TaskID:  id,
			Stdin:   strings.NewReader(""),
			Stdout:  io.Discard,
			Stderr:  io.Discard,
			Agents:  []codingagents.Agent{testutil.NewScriptedAgent()},
			JBinary: jbin,
		},
	); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	args := readArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate",
		"--id", id,
		"--phase=from-work",
		"--interactive=false",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
	_ = testutil.ReadTaskRow(t, id)
}
