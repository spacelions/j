package testcases_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// resumePlanChainAgent records the planner's PlanRequest so the
// resume-plan contract can be black-box pinned: (a) the planner must
// NOT call NewResumeID on a resume run (the row's stored session is
// the source of truth) and (b) the agent must receive Resume=true
// and ResumeChatID=<stored> so the cursor / claude backends select
// BuildPlannerResume and forward `--resume <id>` to the underlying
// CLI. Worker / Verify return errors because the test pairs
// PlanRequiresApproval=true with a plan-pending-approval start
// state, so the orchestrator stops after the planner phase and
// neither downstream method fires.
type resumePlanChainAgent struct {
	planCalls            atomic.Int32
	planNewResumeIDCalls atomic.Int32
	lastPlanReq          codingagents.PlanRequest
	plannerEntered       atomic.Bool
}

func (a *resumePlanChainAgent) Name() string                                 { return "scripted" }
func (a *resumePlanChainAgent) ListModels(context.Context) ([]string, error) { return []string{"m1"}, nil }
func (a *resumePlanChainAgent) CheckLogin(context.Context) error             { return nil }

func (a *resumePlanChainAgent) NewResumeID(context.Context) (string, error) {
	if a.plannerEntered.Load() {
		a.planNewResumeIDCalls.Add(1)
	}
	return "freshly-minted", nil
}

func (a *resumePlanChainAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	a.plannerEntered.Store(true)
	a.planCalls.Add(1)
	a.lastPlanReq = req
	if err := os.WriteFile(req.RequirementsOutputPath, []byte("# task\nbody"), 0o644); err != nil {
		return 0, err
	}
	if err := os.WriteFile(req.PlanOutputPath, []byte("1. step"), 0o644); err != nil {
		return 0, err
	}
	return 0, nil
}

func (a *resumePlanChainAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	panic("Work must not be called: orchestrator should stop at " +
		"plan-pending-approval when PlanRequiresApproval=true")
}

func (a *resumePlanChainAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	panic("Verify must not be called: orchestrator should stop at " +
		"plan-pending-approval when PlanRequiresApproval=true")
}

// putProjectKey writes a project-bucket setting (max_iterations,
// plan_requires_approval, ...). Mirrors the orchestrate-test helper
// at internal/cli/tasks/orchestrate_test.go:writeBucketKey but lives
// here so the testcases package stays self-contained.
func putProjectKey(t *testing.T, key, value string) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Put(store.BucketProject, key, value); err != nil {
		t.Fatalf("Put %s.%s: %v", store.BucketProject, key, err)
	}
}

// TestVerify_ResumePlan_OrchestratorReusesPriorSession is the
// black-box pin for SPA-33: a row whose PlanResumeSession was stamped
// by a prior plan run MUST be picked up verbatim when the orchestrator
// re-runs the planner phase. The agent's NewResumeID is forbidden
// from firing (the row owns the session id), the PlanRequest must
// carry Resume=true and ResumeChatID equal to the stored value (so
// the cursor / claude backend selects BuildPlannerResume and
// forwards `--resume <id>` to the CLI), and the row's stored session
// must remain unchanged after the run so a subsequent resume keeps
// the same chat lineage.
func TestVerify_ResumePlan_OrchestratorReusesPriorSession(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	for _, b := range []string{
		store.BucketPlanner, store.BucketWorker, store.BucketVerifier,
	} {
		testutil.SeedAgentBucketToolModel(t, b, "scripted", "m1")
	}
	// The resume-plan CLI re-execs orchestrate with
	// --plan-requires-approval=true so the chain stops after the
	// planner phase (the user re-approves before worker fires).
	// Mirroring that here keeps the test scoped to the planner
	// inference contract.
	putProjectKey(t, store.KeyPlanRequiresApproval, "true")

	id := tasks.NewTaskID()
	dir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, tasks.RequirementsFileName),
		[]byte("# task\nbody"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	testutil.SeedTaskRow(t, tasks.Task{
		ID:                id,
		Status:            tasks.StatusPlanPendingApproval,
		PlanTool:          "scripted",
		PlanModel:         "m1",
		PlanResumeSession: "prior-cursor",
		Summary:           "spa-33",
	})

	agent := &resumePlanChainAgent{}

	if err := clitasks.RunOrchestrate(
		context.Background(),
		clitasks.OrchestrateOptions{
			TaskID: id,
			Stdin:  strings.NewReader(""),
			Stdout: io.Discard,
			Stderr: io.Discard,
			Agents: []codingagents.Agent{agent},
		},
	); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	if agent.planCalls.Load() != 1 {
		t.Fatalf("plan calls = %d, want 1", agent.planCalls.Load())
	}
	if agent.planNewResumeIDCalls.Load() != 0 {
		t.Fatalf(
			"NewResumeID calls during planner phase = %d, want 0 "+
				"on resume run (stored session is the source "+
				"of truth)",
			agent.planNewResumeIDCalls.Load(),
		)
	}
	if !agent.lastPlanReq.Resume {
		t.Fatal(
			"PlanRequest.Resume = false, want true so the backend " +
				"selects BuildPlannerResume (planner_resume.md)",
		)
	}
	if agent.lastPlanReq.ResumeChatID != "prior-cursor" {
		t.Fatalf(
			"PlanRequest.ResumeChatID = %q, want prior-cursor "+
				"so the backend forwards --resume prior-cursor "+
				"to the underlying CLI",
			agent.lastPlanReq.ResumeChatID,
		)
	}
	row := testutil.ReadTaskRow(t, id)
	if row.PlanResumeSession != "prior-cursor" {
		t.Fatalf(
			"PlanResumeSession = %q, want prior-cursor "+
				"(resume must NOT overwrite the row's stored session)",
			row.PlanResumeSession,
		)
	}
}

