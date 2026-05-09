package testcases_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/worker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestClarificationResume_WorkerHappyPath_FinishesWorkDone pins
// AC1+AC2+AC3+AC5+AC6 for the worker phase: a `j tasks continue`
// resume on a needs-clarification row whose latest phase was the
// worker must:
//   - propagate `ResumeFromClarification=true` on WorkRequest so the
//     coding-agent backend selects the resume-from-clarification
//     prompt template (AC2 / AC1);
//   - on a clean exit where the agent deletes clarification.md,
//     finalise the row at the natural terminal status `work-done`
//     (AC3) so the orchestrator chain proceeds into verify.
//
// Black-box: the captured WorkRequest pins flag propagation; the
// stub deletes clarification.md before returning so
// WorkLifecycle.Finish routes via EventWorkDone instead of
// EventWorkNeedsClarification.
func TestClarificationResume_WorkerHappyPath_FinishesWorkDone(
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
		filepath.Join(taskDir, tasks.PlanFileName),
		[]byte("1. do thing"), 0o644,
	); err != nil {
		t.Fatalf("write plan.md: %v", err)
	}
	clar := filepath.Join(taskDir, tasks.ClarificationFileName)
	if err := os.WriteFile(
		clar, []byte("which lib?"), 0o644,
	); err != nil {
		t.Fatalf("write clarification.md: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketWorker, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:                id,
		Status:            tasks.StatusWorking,
		WorkTool:          "scripted",
		WorkModel:         "m1",
		WorkResumeSession: "prior-cursor",
		Summary:           "task",
	})

	stub := &deletingWorkAgent{name: "scripted", clarPath: clar}
	if err := worker.Execute(t.Context(), worker.ExecuteOptions{
		TaskID: id,
		Yes:    true,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{stub},
		UI:     &noopWorkerUI{},
		Tool:   "scripted",
		Model:  "m1",
	}); err != nil {
		t.Fatalf("worker.Execute: %v", err)
	}
	if !stub.lastReq.Resume {
		t.Fatal("WorkRequest.Resume = false, want true on resume")
	}
	if !stub.lastReq.ResumeFromClarification {
		t.Fatal(
			"WorkRequest.ResumeFromClarification = false, " +
				"want true with clarification.md present",
		)
	}
	got := testutil.ReadTaskRow(t, id)
	if got.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
	if _, statErr := os.Stat(clar); statErr == nil {
		t.Fatal("clarification.md should be deleted after resume")
	}
}

type deletingWorkAgent struct {
	name     string
	clarPath string
	lastReq  codingagents.WorkRequest
}

func (a *deletingWorkAgent) Name() string { return a.name }

func (a *deletingWorkAgent) ListModels(
	context.Context,
) ([]string, error) {
	return []string{"m1"}, nil
}

func (a *deletingWorkAgent) CheckLogin(context.Context) error {
	return nil
}

func (a *deletingWorkAgent) NewResumeID(
	context.Context,
) (string, error) {
	return "rid", nil
}

func (a *deletingWorkAgent) Plan(
	context.Context, codingagents.PlanRequest,
) (int, error) {
	return 0, errors.New("Plan must not be called from worker test")
}

func (a *deletingWorkAgent) Work(
	_ context.Context, req codingagents.WorkRequest,
) (int, error) {
	a.lastReq = req
	if err := os.Remove(a.clarPath); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	return 0, nil
}

func (a *deletingWorkAgent) Verify(
	context.Context, codingagents.VerifyRequest,
) (int, error) {
	return 0, errors.New("Verify must not be called from worker test")
}

func (*deletingWorkAgent) FormatLog(line []byte) []byte { return line }

// noopWorkerUI satisfies worker.UI without exercising any prompts;
// every call returns a zero value because the worker.Execute path
// driven here goes through TaskID + explicit Tool / Model and
// confirmation is bypassed via Yes=true.
type noopWorkerUI struct{}

func (noopWorkerUI) PickTask(
	context.Context, string, []tasks.Task,
) (string, bool, error) {
	return "", false, nil
}

func (noopWorkerUI) SelectTool(
	context.Context, []string,
) (string, error) {
	return "", nil
}

func (noopWorkerUI) SelectModel(
	context.Context, []string,
) (string, error) {
	return "", nil
}

func (noopWorkerUI) ConfirmStatusOverride(
	context.Context, string, string, string,
) (bool, error) {
	return true, nil
}
