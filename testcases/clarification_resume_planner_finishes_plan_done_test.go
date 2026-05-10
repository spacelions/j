package testcases_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/agents/planner"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestClarificationResume_PlannerHappyPath_FinishesPlanDone pins
// AC1+AC2+AC3+AC5+AC6 for the planner phase: when the user runs
// `j tasks continue` on a needs-clarification task whose planner
// produced the question, the planner.Execute resume run must
//   - propagate `ResumeFromClarification=true` on PlanRequest so the
//     coding-agent backend can pick the resume-from-clarification
//     prompt template (AC2 / AC1 — same TUI, pointed at the
//     question);
//   - on a clean exit where the agent deletes clarification.md,
//     finalise the row at the natural terminal status `plan-done`
//     (AC3) — not `needs-clarification`.
//
// Black-box: the agent stub captures the request, deletes
// clarification.md, and writes the requirements / plan artifacts
// the planner lifecycle expects.
func TestClarificationResume_PlannerHappyPath_FinishesPlanDone(
	t *testing.T,
) {
	freshInit(t)
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)

	id := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(taskDir, tasks.RequirementsFileName),
		[]byte("# task\nbody"), 0o644,
	); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	clar := filepath.Join(taskDir, tasks.ClarificationFileName)
	if err := os.WriteFile(
		clar, []byte("which framework should I use?"), 0o644,
	); err != nil {
		t.Fatalf("write clarification: %v", err)
	}
	testutil.SeedAgentBucket(
		t, store.BucketPlanner, "scripted", "m1",
	)
	testutil.SeedTaskRow(t, tasks.Task{
		ID:                id,
		Status:            tasks.StatusPlanning,
		PlanTool:          "scripted",
		PlanModel:         "m1",
		PlanResumeSession: "prior-cursor",
		Summary:           "task",
	})

	stub := &deletingPlanAgent{name: "scripted", clarPath: clar}
	if err := planner.Execute(t.Context(), planner.Options{
		TaskID: id,
		Agent:  stub,
		Model:  "m1",
		Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("planner.Execute: %v", err)
	}
	if !stub.lastReq.Resume {
		t.Fatal("PlanRequest.Resume = false, want true on resume")
	}
	if !stub.lastReq.ResumeFromClarification {
		t.Fatal(
			"PlanRequest.ResumeFromClarification = false, " +
				"want true with clarification.md present",
		)
	}
	got := testutil.ReadTaskRow(t, id)
	if got.Status != tasks.StatusPlanDone &&
		got.Status != tasks.StatusPlanPendingApproval {
		t.Fatalf(
			"Status = %q, want plan-done or plan-pending-approval",
			got.Status,
		)
	}
	if _, statErr := os.Stat(clar); statErr == nil {
		t.Fatal("clarification.md should be deleted after resume")
	}
}

// deletingPlanAgent stands in for a coding-agent whose planner
// session has answered the user's clarification: it deletes
// clarification.md and writes the canonical artifacts so the
// lifecycle finalises at plan-done.
type deletingPlanAgent struct {
	name     string
	clarPath string
	lastReq  codingagents.PlanRequest
}

func (a *deletingPlanAgent) Name() string { return a.name }

func (a *deletingPlanAgent) ListModels(
	context.Context,
) ([]string, error) {
	return []string{"m1"}, nil
}

func (a *deletingPlanAgent) CheckLogin(context.Context) error {
	return nil
}

func (a *deletingPlanAgent) NewResumeID(
	context.Context,
) (string, error) {
	return "rid", nil
}

func (a *deletingPlanAgent) Plan(
	_ context.Context, req codingagents.PlanRequest,
) (int, error) {
	a.lastReq = req
	if err := os.Remove(a.clarPath); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	if err := os.WriteFile(
		req.RequirementsOutputPath,
		[]byte("refined requirements one-liner"), 0o644,
	); err != nil {
		return 0, err
	}
	if err := os.WriteFile(
		req.PlanOutputPath, []byte("1. step"), 0o644,
	); err != nil {
		return 0, err
	}
	return 0, nil
}

func (a *deletingPlanAgent) Work(
	context.Context, codingagents.WorkRequest,
) (int, error) {
	return 0, errors.New("Work must not be called from planner test")
}

func (a *deletingPlanAgent) Verify(
	context.Context, codingagents.VerifyRequest,
) (int, error) {
	return 0, errors.New("Verify must not be called from planner test")
}

func (*deletingPlanAgent) FormatLog(line []byte) []byte { return line }
