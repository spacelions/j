package testcases_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksContinue_ForwardsToolModelToOrchestrator pins that the
// --tool / --model overrides on `j tasks continue` flow through into
// the spawned orchestrator argv on the plan-done dispatch path. Per
// the SPA-30 fix scope the orchestrator child resolves agent buckets
// itself; passing the user's overrides in lets the child preflight
// surface tool/model problems instead of silently falling back.
func TestTasksContinue_ForwardsToolModelToOrchestrator(t *testing.T) {
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
		Summary:           "spa-30 flag forwarding",
		PlanBeginAt:       begin,
		PlanEndAt:         begin.Add(time.Hour),
	})

	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	jbin := argvJStub(t, argvPath)
	if err := clitasks.RunContinue(
		context.Background(),
		clitasks.ContinueOptions{
			TaskID:  id,
			Tool:    "claude",
			Model:   "opus-4",
			Stdin:   strings.NewReader(""),
			Stdout:  io.Discard,
			Stderr:  io.Discard,
			Agents:  []codingagents.Agent{testutil.NewScriptedAgent()},
			JBinary: jbin,
		},
	); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	joined := strings.Join(readArgv(t, argvPath), " ")
	for _, want := range []string{
		"--phase=from-work",
		"--tool=claude",
		"--model=opus-4",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("argv = %q, want substring %q", joined, want)
		}
	}
}
