package testcases_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/planner"
	"github.com/spacelions/j/internal/agents/verifier"
	"github.com/spacelions/j/internal/agents/worker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestClarificationResume_ReRaise_RoutesBackToNeedsClarification
// pins AC4 + AC5 symmetry: when the resumed agent decides the
// user's reply is still insufficient and rewrites (or leaves)
// `clarification.md`, the lifecycle MUST route the row back to
// `needs-clarification` so the user can answer again on the next
// `j tasks continue`. Otherwise the user is silently stuck (the
// task ends in `plan-done`/`work-done`/`completed` while the
// open question persists on disk).
//
// One subtest per phase pins the symmetry across planner /
// worker / verifier — the AC requires uniform behaviour on
// every `j tasks continue` from `needs-clarification`.
func TestClarificationResume_ReRaise_RoutesBackToNeedsClarification(
	t *testing.T,
) {
	t.Run("planner", func(t *testing.T) {
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
			clar, []byte("first question?"), 0o644,
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

		stub := &reraisePlanAgent{
			name:     "scripted",
			clarPath: clar,
			newBody:  "still need more detail",
		}
		if err := planner.Execute(
			t.Context(), planner.ExecuteOptions{
				TaskID: id,
				Agent:  stub,
				Model:  "m1",
				Stderr: io.Discard,
			},
		); err != nil {
			t.Fatalf("planner.Execute: %v", err)
		}
		got := testutil.ReadTaskRow(t, id)
		if got.Status != tasks.StatusNeedsClarification {
			t.Fatalf(
				"Status = %q, want needs-clarification "+
					"(re-raised question)",
				got.Status,
			)
		}
	})

	t.Run("worker", func(t *testing.T) {
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
			[]byte("1. step"), 0o644,
		); err != nil {
			t.Fatalf("write plan.md: %v", err)
		}
		clar := filepath.Join(taskDir, tasks.ClarificationFileName)
		if err := os.WriteFile(
			clar, []byte("which lib?"), 0o644,
		); err != nil {
			t.Fatalf("write clarification: %v", err)
		}
		testutil.SeedAgentBucket(
			t, store.BucketWorker, "scripted", "m1",
		)
		testutil.SeedTaskRow(t, tasks.Task{
			ID:                id,
			Status:            tasks.StatusWorking,
			WorkTool:          "scripted",
			WorkModel:         "m1",
			WorkResumeSession: "prior-cursor",
			Summary:           "task",
		})

		stub := &reraiseWorkAgent{
			name:     "scripted",
			clarPath: clar,
			newBody:  "need more from you",
		}
		if err := worker.Execute(
			t.Context(), worker.ExecuteOptions{
				TaskID: id,
				Yes:    true,
				Stdin:  strings.NewReader(""),
				Stdout: io.Discard,
				Stderr: io.Discard,
				Agents: []codingagents.Agent{stub},
				UI:     &noopWorkerUI{},
				Tool:   "scripted",
				Model:  "m1",
			},
		); err != nil {
			t.Fatalf("worker.Execute: %v", err)
		}
		got := testutil.ReadTaskRow(t, id)
		if got.Status != tasks.StatusNeedsClarification {
			t.Fatalf(
				"Status = %q, want needs-clarification "+
					"(re-raised question)",
				got.Status,
			)
		}
	})

	t.Run("verifier", func(t *testing.T) {
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
			clar, []byte("which behaviour?"), 0o644,
		); err != nil {
			t.Fatalf("write clarification: %v", err)
		}
		testutil.SeedTaskRow(t, tasks.Task{
			ID:                  id,
			Status:              tasks.StatusVerifying,
			VerifyTool:          "scripted",
			VerifyModel:         "m1",
			VerifyResumeSession: "prior-cursor",
			Summary:             "task",
		})

		stub := &reraiseVerifyAgent{
			name:     "scripted",
			clarPath: clar,
			newBody:  "still need more detail",
		}
		if err := verifier.RunResume(
			t.Context(), verifier.ResumeOptions{
				TaskID: id,
				Stdout: io.Discard,
				Stderr: io.Discard,
				Agents: []codingagents.Agent{stub},
			},
		); err != nil {
			t.Fatalf("verifier.RunResume: %v", err)
		}
		got := testutil.ReadTaskRow(t, id)
		if got.Status != tasks.StatusNeedsClarification {
			t.Fatalf(
				"Status = %q, want needs-clarification "+
					"(re-raised question)",
				got.Status,
			)
		}
	})
}

type reraisePlanAgent struct {
	name     string
	clarPath string
	newBody  string
	lastReq  codingagents.PlanRequest
}

func (a *reraisePlanAgent) Name() string { return a.name }

func (a *reraisePlanAgent) ListModels(
	context.Context,
) ([]string, error) {
	return []string{"m1"}, nil
}

func (a *reraisePlanAgent) CheckLogin(context.Context) error {
	return nil
}

func (a *reraisePlanAgent) NewResumeID(
	context.Context,
) (string, error) {
	return "rid", nil
}

func (a *reraisePlanAgent) Plan(
	_ context.Context, req codingagents.PlanRequest,
) (int, error) {
	a.lastReq = req
	if err := os.WriteFile(
		a.clarPath, []byte(a.newBody), 0o644,
	); err != nil {
		return 0, err
	}
	return 0, nil
}

func (a *reraisePlanAgent) Work(
	context.Context, codingagents.WorkRequest,
) (int, error) {
	return 0, errors.New("Work must not be called")
}

func (a *reraisePlanAgent) Verify(
	context.Context, codingagents.VerifyRequest,
) (int, error) {
	return 0, errors.New("Verify must not be called")
}

func (*reraisePlanAgent) FormatLog(line []byte) []byte { return line }

type reraiseWorkAgent struct {
	name     string
	clarPath string
	newBody  string
	lastReq  codingagents.WorkRequest
}

func (a *reraiseWorkAgent) Name() string { return a.name }

func (a *reraiseWorkAgent) ListModels(
	context.Context,
) ([]string, error) {
	return []string{"m1"}, nil
}

func (a *reraiseWorkAgent) CheckLogin(context.Context) error {
	return nil
}

func (a *reraiseWorkAgent) NewResumeID(
	context.Context,
) (string, error) {
	return "rid", nil
}

func (a *reraiseWorkAgent) Plan(
	context.Context, codingagents.PlanRequest,
) (int, error) {
	return 0, errors.New("Plan must not be called")
}

func (a *reraiseWorkAgent) Work(
	_ context.Context, req codingagents.WorkRequest,
) (int, error) {
	a.lastReq = req
	if err := os.WriteFile(
		a.clarPath, []byte(a.newBody), 0o644,
	); err != nil {
		return 0, err
	}
	return 0, nil
}

func (a *reraiseWorkAgent) Verify(
	context.Context, codingagents.VerifyRequest,
) (int, error) {
	return 0, errors.New("Verify must not be called")
}

func (*reraiseWorkAgent) FormatLog(line []byte) []byte { return line }

type reraiseVerifyAgent struct {
	name     string
	clarPath string
	newBody  string
	lastReq  codingagents.VerifyRequest
}

func (a *reraiseVerifyAgent) Name() string { return a.name }

func (a *reraiseVerifyAgent) ListModels(
	context.Context,
) ([]string, error) {
	return []string{"m1"}, nil
}

func (a *reraiseVerifyAgent) CheckLogin(context.Context) error {
	return nil
}

func (a *reraiseVerifyAgent) NewResumeID(
	context.Context,
) (string, error) {
	return "rid", nil
}

func (a *reraiseVerifyAgent) Plan(
	context.Context, codingagents.PlanRequest,
) (int, error) {
	return 0, errors.New("Plan must not be called")
}

func (a *reraiseVerifyAgent) Work(
	context.Context, codingagents.WorkRequest,
) (int, error) {
	return 0, errors.New("Work must not be called")
}

func (a *reraiseVerifyAgent) Verify(
	_ context.Context, req codingagents.VerifyRequest,
) (int, error) {
	a.lastReq = req
	if err := os.WriteFile(
		a.clarPath, []byte(a.newBody), 0o644,
	); err != nil {
		return 0, err
	}
	return 0, nil
}

func (*reraiseVerifyAgent) FormatLog(line []byte) []byte { return line }
