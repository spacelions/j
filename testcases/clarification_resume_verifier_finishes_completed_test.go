package testcases_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/agents/verifier"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestClarificationResume_VerifierHappyPath_FinishesCompleted pins
// AC1+AC2+AC3+AC5+AC6 for the verifier phase. RunResume on a
// needs-clarification row whose verifier emitted the question must:
//   - propagate `ResumeFromClarification=true` on VerifyRequest so
//     the coding-agent backend selects the resume-from-clarification
//     prompt template (AC2);
//   - on a clean exit where the agent deletes clarification.md and
//     writes a `VERDICT: PASS` findings file, finalise the row at
//     the natural terminal status `completed` (AC3).
//
// Black-box: the captured VerifyRequest pins flag propagation; the
// stub deletes clarification.md and writes a PASS verdict so
// VerifyLifecycle.Finish picks EventVerifyPass.
func TestClarificationResume_VerifierHappyPath_FinishesCompleted(
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
		[]byte("# req\nbody"), 0o644,
	); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(taskDir, tasks.PlanFileName),
		[]byte("1. step"), 0o644,
	); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	clar := filepath.Join(taskDir, tasks.ClarificationFileName)
	if err := os.WriteFile(
		clar, []byte("which behaviour is correct?"), 0o644,
	); err != nil {
		t.Fatalf("write clarification: %v", err)
	}
	findings := filepath.Join(taskDir, tasks.VerifierFindingsFileName)
	testutil.SeedTaskRow(t, tasks.Task{
		ID:                  id,
		Status:              tasks.StatusVerifying,
		VerifyTool:          "scripted",
		VerifyModel:         "m1",
		VerifyResumeSession: "prior-cursor",
		Summary:             "task",
	})

	stub := &deletingVerifyAgent{
		name:         "scripted",
		clarPath:     clar,
		findingsPath: findings,
	}
	if err := verifier.RunResume(t.Context(), verifier.ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("verifier.RunResume: %v", err)
	}
	if !stub.lastReq.Resume {
		t.Fatal("VerifyRequest.Resume = false, want true on resume")
	}
	if !stub.lastReq.ResumeFromClarification {
		t.Fatal(
			"VerifyRequest.ResumeFromClarification = false, " +
				"want true with clarification.md present",
		)
	}
	got := testutil.ReadTaskRow(t, id)
	if got.Status != tasks.StatusCompleted {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
	if _, statErr := os.Stat(clar); statErr == nil {
		t.Fatal("clarification.md should be deleted after resume")
	}
}

type deletingVerifyAgent struct {
	name         string
	clarPath     string
	findingsPath string
	lastReq      codingagents.VerifyRequest
}

func (a *deletingVerifyAgent) Name() string { return a.name }

func (a *deletingVerifyAgent) ListModels(
	context.Context,
) ([]string, error) {
	return []string{"m1"}, nil
}

func (a *deletingVerifyAgent) CheckLogin(context.Context) error {
	return nil
}

func (a *deletingVerifyAgent) NewResumeID(
	context.Context,
) (string, error) {
	return "rid", nil
}

func (a *deletingVerifyAgent) Plan(
	context.Context, codingagents.PlanRequest,
) (int, error) {
	return 0, errors.New("Plan must not be called from verifier test")
}

func (a *deletingVerifyAgent) Work(
	context.Context, codingagents.WorkRequest,
) (int, error) {
	return 0, errors.New("Work must not be called from verifier test")
}

func (a *deletingVerifyAgent) Verify(
	_ context.Context, req codingagents.VerifyRequest,
) (int, error) {
	a.lastReq = req
	if err := os.Remove(a.clarPath); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	if err := os.WriteFile(
		req.VerifierFindingsOutputPath,
		[]byte("- ok\nVERDICT: PASS\n"), 0o644,
	); err != nil {
		return 0, err
	}
	return 0, nil
}
