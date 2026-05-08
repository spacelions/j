package testcases_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/agents/planner"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// resumePlanCaptureAgent records the PlanRequest delivered to Plan
// and panics if NewResumeID is invoked. It writes the requirements /
// plan files inline so planner.Execute can promote the row to its
// terminal status without a real coding-agent subprocess.
type resumePlanCaptureAgent struct {
	planCalls   int
	resumeCalls int
	lastReq     codingagents.PlanRequest
}

func (a *resumePlanCaptureAgent) Name() string { return "scripted" }
func (a *resumePlanCaptureAgent) ListModels(context.Context) ([]string, error) {
	return []string{"m1"}, nil
}
func (a *resumePlanCaptureAgent) CheckLogin(context.Context) error { return nil }

func (a *resumePlanCaptureAgent) NewResumeID(context.Context) (string, error) {
	a.resumeCalls++
	panic("NewResumeID must NOT be called when PlanResumeSession is set")
}

func (a *resumePlanCaptureAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	a.planCalls++
	a.lastReq = req
	if err := testutil.WriteFile(req.RequirementsOutputPath, "refined"); err != nil {
		return 0, err
	}
	if err := testutil.WriteFile(req.PlanOutputPath, "1. step"); err != nil {
		return 0, err
	}
	return 0, nil
}

func (a *resumePlanCaptureAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	return 0, errors.New("Work should not be called")
}

func (a *resumePlanCaptureAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("Verify should not be called")
}

// TestVerify_PlannerResume_UsesStoredSession is the black-box pin
// for SPA-33: when the row carries a non-empty PlanResumeSession the
// planner phase MUST (a) skip NewResumeID, (b) deliver the stored
// session to PlanRequest.ResumeChatID, (c) set PlanRequest.Resume so
// the backend selects the BuildPlannerResume framing, and (d)
// preserve the row's PlanResumeSession verbatim afterwards.
func TestVerify_PlannerResume_UsesStoredSession(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	id := tasks.NewTaskID()
	dir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(
		filepath.Join(dir, tasks.RequirementsFileName),
		"# resume\nbody"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucketToolModel(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:                id,
		Status:            tasks.StatusPlanPendingApproval,
		PlanTool:          "scripted",
		PlanModel:         "m1",
		PlanResumeSession: "prior-cursor",
		Summary:           "resume",
	})

	stub := &resumePlanCaptureAgent{}
	if err := planner.Execute(t.Context(),
		planner.ExecuteOptions{
			TaskID: id,
			Agent:  stub,
			Model:  "m1",
			Stderr: io.Discard,
		}); err != nil {
		t.Fatalf("planner.Execute: %v", err)
	}
	if stub.resumeCalls != 0 {
		t.Fatalf("NewResumeID calls = %d, want 0", stub.resumeCalls)
	}
	if stub.planCalls != 1 {
		t.Fatalf("Plan calls = %d, want 1", stub.planCalls)
	}
	if stub.lastReq.ResumeChatID != "prior-cursor" {
		t.Fatalf("ResumeChatID = %q, want prior-cursor",
			stub.lastReq.ResumeChatID)
	}
	if !stub.lastReq.Resume {
		t.Fatal("PlanRequest.Resume = false, want true on resume run")
	}
	row := testutil.ReadTaskRow(t, id)
	if row.PlanResumeSession != "prior-cursor" {
		t.Fatalf(
			"PlanResumeSession = %q, want prior-cursor (must be preserved)",
			row.PlanResumeSession,
		)
	}
}
