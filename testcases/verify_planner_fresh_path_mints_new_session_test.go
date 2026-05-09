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

// freshPlanCaptureAgent records both NewResumeID invocations and the
// PlanRequest. NewResumeID returns a deterministic id ("fresh-rid")
// so the test can assert it propagates into the row and the request.
type freshPlanCaptureAgent struct {
	planCalls   int
	resumeCalls int
	lastReq     codingagents.PlanRequest
}

func (a *freshPlanCaptureAgent) Name() string { return "scripted" }
func (a *freshPlanCaptureAgent) ListModels(context.Context) ([]string, error) {
	return []string{"m1"}, nil
}
func (a *freshPlanCaptureAgent) CheckLogin(context.Context) error { return nil }

func (a *freshPlanCaptureAgent) NewResumeID(context.Context) (string, error) {
	a.resumeCalls++
	return "fresh-rid", nil
}

func (a *freshPlanCaptureAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
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

func (a *freshPlanCaptureAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	return 0, errors.New("Work should not be called")
}

func (a *freshPlanCaptureAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("Verify should not be called")
}

func (*freshPlanCaptureAgent) FormatLog(line []byte) []byte { return line }

// TestVerify_PlannerFresh_MintsNewSessionAndUsesFreshPrompt pins the
// fresh-run contract: a row with an empty PlanResumeSession (the
// shape `j tasks start` writes) MUST trigger NewResumeID, set
// PlanRequest.Resume=false so the backend selects the BuildPlanner
// framing (NOT BuildPlannerResume), and stamp the freshly-minted id
// onto the row so a subsequent resume-plan can pick it up.
func TestVerify_PlannerFresh_MintsNewSessionAndUsesFreshPrompt(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	id := tasks.NewTaskID()
	dir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(
		filepath.Join(dir, tasks.RequirementsFileName),
		"# fresh\nbody"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucketToolModel(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:        id,
		Status:    tasks.StatusPlanning,
		PlanTool:  "scripted",
		PlanModel: "m1",
		Summary:   "fresh",
	})

	stub := &freshPlanCaptureAgent{}
	if err := planner.Execute(t.Context(),
		planner.ExecuteOptions{
			TaskID: id,
			Agent:  stub,
			Model:  "m1",
			Stderr: io.Discard,
		}); err != nil {
		t.Fatalf("planner.Execute: %v", err)
	}
	if stub.resumeCalls != 1 {
		t.Fatalf("NewResumeID calls = %d, want 1", stub.resumeCalls)
	}
	if stub.planCalls != 1 {
		t.Fatalf("Plan calls = %d, want 1", stub.planCalls)
	}
	if stub.lastReq.ResumeChatID != "fresh-rid" {
		t.Fatalf(
			"ResumeChatID = %q, want fresh-rid (newly minted)",
			stub.lastReq.ResumeChatID,
		)
	}
	if stub.lastReq.Resume {
		t.Fatal("PlanRequest.Resume = true, want false on fresh run")
	}
	row := testutil.ReadTaskRow(t, id)
	if row.PlanResumeSession != "fresh-rid" {
		t.Fatalf(
			"PlanResumeSession = %q, want fresh-rid (newly minted)",
			row.PlanResumeSession,
		)
	}
}
