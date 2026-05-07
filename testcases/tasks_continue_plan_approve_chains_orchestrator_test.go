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

// TestTasksContinue_PlanApproveChainsToOrchestratorFromWork pins that
// `j tasks continue` on a `plan-pending-approval` row inherits the
// SPA-30 fix transparently: the implicit approval transitions the row
// to `plan-done` and then dispatches through the orchestrator with
// `--phase=from-work`, the same code path the bare plan-done row
// follows. The legacy buggy in-process worker shell-out is gone.
func TestTasksContinue_PlanApproveChainsToOrchestratorFromWork(t *testing.T) {
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
		Status:            tasks.StatusPlanPendingApproval,
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		WorkTool:          "cursor",
		WorkModel:         "sonnet-4",
		VerifyTool:        "cursor",
		VerifyModel:       "sonnet-4",
		PlanResumeSession: "plan-cursor",
		Summary:           "spa-30 plan-approve",
		PlanBeginAt:       begin,
		PlanEndAt:         begin.Add(time.Hour),
	})

	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	jbin := argvJStub(t, argvPath)
	if err := clitasks.RunContinue(
		context.Background(),
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
	if !strings.Contains(strings.Join(args, " "), "--phase=from-work") {
		t.Fatalf("argv = %v, want --phase=from-work", args)
	}
	if !strings.Contains(strings.Join(args, " "), "--id "+id) {
		t.Fatalf("argv = %v, want --id %s", args, id)
	}
	row := testutil.ReadTaskRow(t, id)
	if row.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done after EventPlanApprove",
			row.Status)
	}
}
