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

// TestClarificationResume_FreshPlannerRun_DoesNotSetFlag pins
// AC7 + AC1 negative-symmetry: a fresh planner run (no prior
// PlanResumeSession on the row) MUST NOT set
// `ResumeFromClarification` even when a stale `clarification.md`
// happens to be on disk. Otherwise the agent would wake up
// believing it owes the user an answer it never asked, breaking
// the "fresh start keeps current behaviour" contract.
func TestClarificationResume_FreshPlannerRun_DoesNotSetFlag(
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
	stale := filepath.Join(taskDir, tasks.ClarificationFileName)
	if err := os.WriteFile(
		stale, []byte("stale leftover"), 0o644,
	); err != nil {
		t.Fatalf("write stale clarification: %v", err)
	}
	testutil.SeedAgentBucket(
		t, store.BucketPlanner, "scripted", "m1",
	)
	testutil.SeedTaskRow(t, tasks.Task{
		ID:        id,
		Status:    tasks.StatusPlanning,
		PlanTool:  "scripted",
		PlanModel: "m1",
		Summary:   "task",
	})

	stub := &capturingPlanAgent{name: "scripted"}
	if err := planner.Execute(t.Context(), planner.Options{
		TaskID: id,
		Agent:  stub,
		Model:  "m1",
		Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("planner.Execute: %v", err)
	}
	if stub.lastReq.Resume {
		t.Fatal(
			"PlanRequest.Resume = true on a fresh run, " +
				"want false",
		)
	}
	if stub.lastReq.ResumeFromClarification {
		t.Fatal(
			"PlanRequest.ResumeFromClarification = true on a " +
				"fresh run, want false (AC7: existing flows " +
				"are unchanged)",
		)
	}
}

type capturingPlanAgent struct {
	name    string
	lastReq codingagents.PlanRequest
}

func (a *capturingPlanAgent) Name() string { return a.name }

func (a *capturingPlanAgent) ListModels(
	context.Context,
) ([]string, error) {
	return []string{"m1"}, nil
}

func (a *capturingPlanAgent) CheckLogin(context.Context) error {
	return nil
}

func (a *capturingPlanAgent) NewResumeID(
	context.Context,
) (string, error) {
	return "rid", nil
}

func (a *capturingPlanAgent) Plan(
	_ context.Context, req codingagents.PlanRequest,
) (int, error) {
	a.lastReq = req
	if err := os.WriteFile(
		req.RequirementsOutputPath,
		[]byte("refined requirements"), 0o644,
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

func (a *capturingPlanAgent) Work(
	context.Context, codingagents.WorkRequest,
) (int, error) {
	return 0, errors.New("Work must not be called")
}

func (a *capturingPlanAgent) Verify(
	context.Context, codingagents.VerifyRequest,
) (int, error) {
	return 0, errors.New("Verify must not be called")
}

func (*capturingPlanAgent) FormatLog(line []byte) []byte { return line }
