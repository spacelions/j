package lifecycle

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// listAllTasks lists every task at the per-cwd tasks dir. Used by
// lifecycle tests to assert what the PersistWarn-driven helpers
// wrote. Returns nil for "no tasks dir yet" so the negative-path
// tests can distinguish "missing" from a real read error.
func listAllTasks(t *testing.T) []tasks.Task {
	t.Helper()
	dir := tasks.DefaultDir()
	if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	s := tasks.Open(dir)
	defer func() { _ = s.Close() }()
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	return got
}

// seedPlanDoneTask seeds a `plan-done` row for the work / verify
// lifecycle tests. The id is returned so callers can look the row
// back up. Use after t.Chdir(t.TempDir()) + store.EnsureProject().
func seedPlanDoneTask(t *testing.T, summary string) string {
	t.Helper()
	id := tasks.NewTaskID()
	dir := tasks.DefaultDir()
	s := tasks.Open(dir)
	defer func() { _ = s.Close() }()
	begin := time.Now().UTC().Add(-time.Hour)
	end := begin.Add(time.Minute)
	task := tasks.Task{
		ID:                id,
		Status:            tasks.StatusPlanDone,
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		PlanResumeSession: "seed-plan-cursor",
		Summary:           summary,
		PlanBeginAt:       begin,
		PlanEndAt:         end,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}

// seedWorkDoneTask seeds a `work-done` row for the verify lifecycle
// tests. Mirrors seedPlanDoneTask's shape but with the work-phase
// timestamps and resume cursor populated.
func seedWorkDoneTask(t *testing.T, summary string) string {
	t.Helper()
	id := tasks.NewTaskID()
	dir := tasks.DefaultDir()
	s := tasks.Open(dir)
	defer func() { _ = s.Close() }()
	planBegin := time.Now().UTC().Add(-2 * time.Hour)
	planEnd := planBegin.Add(time.Minute)
	workBegin := planEnd.Add(time.Minute)
	workEnd := workBegin.Add(time.Minute)
	task := tasks.Task{
		ID:                id,
		Status:            tasks.StatusWorkDone,
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		WorkTool:          "cursor",
		WorkModel:         "sonnet-4",
		PlanResumeSession: "seed-plan-cursor",
		WorkResumeSession: "seed-work-cursor",
		Summary:           summary,
		PlanBeginAt:       planBegin,
		PlanEndAt:         planEnd,
		WorkBeginAt:       workBegin,
		WorkEndAt:         workEnd,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}

// seedPlanApprovalDisabled writes plan_requires_approval=false to the
// project settings store so PlanLifecycle.Finish(nil) uses EventPlanDone
// instead of the default EventPlanAwaitApproval. Call after EnsureProject.
func seedPlanApprovalDisabled(t *testing.T) {
	t.Helper()
	seedPlanApproval(t, "false")
}

// seedPlanApprovalEnabled writes plan_requires_approval=true so
// PlanLifecycle.Finish(nil) routes to EventPlanAwaitApproval.
func seedPlanApprovalEnabled(t *testing.T) {
	t.Helper()
	seedPlanApproval(t, "true")
}

func seedPlanApproval(t *testing.T, value string) {
	t.Helper()
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open settings: %v", err)
	}
	defer s.Close()
	if err := s.Put(store.BucketProject,
		store.KeyPlanRequiresApproval, value); err != nil {
		t.Fatalf("Put plan_requires_approval: %v", err)
	}
}

func newPlanTaskTest(
	stderr io.Writer,
	agentName, model, taskID, target string,
	requirement, resumeID, agentLogPath string,
	linearIssue string,
) *PlanLifecycle {
	lc := NewPlanTask(stderr, taskID, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	}, PlanSource{
		Requirement: requirement,
		Target:      target,
		LinearIssue: linearIssue,
	})
	setPlanLogPath(stderr, lc, agentLogPath, tasks.EventPlanBegin)
	return lc
}

func beginPlanRestartTest(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, resumeID, agentLogPath string,
) *PlanLifecycle {
	lc := BeginPlanRestart(t, stderr, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	})
	setPlanLogPath(stderr, lc, agentLogPath, tasks.EventPlanRestart)
	return lc
}

func beginPlanResumeTest(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, agentLogPath string,
) *PlanLifecycle {
	lc := BeginPlanResume(t, stderr, codingagents.AgentSession{
		Tool:  agentName,
		Model: model,
	})
	setPlanLogPath(stderr, lc, agentLogPath, tasks.EventPlanResume)
	return lc
}

func beginPlanExistingTest(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, resumeID, agentLogPath string,
) *PlanLifecycle {
	lc := BeginPlanExisting(t, stderr, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	})
	setPlanLogPath(stderr, lc, agentLogPath, tasks.EventPlanBegin)
	return lc
}

func newWorkTaskTest(
	stderr io.Writer,
	agentName, model, taskID, planPath string,
	requirement, planBody, resumeID, agentLogPath string,
) *WorkLifecycle {
	lc := NewWorkTask(stderr, taskID, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	}, WorkSource{
		Requirement: requirement,
		PlanBody:    planBody,
		PlanPath:    planPath,
	})
	setWorkLogPath(stderr, lc, agentLogPath, tasks.EventWorkBegin)
	return lc
}

func beginWorkRestartTest(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, resumeID, agentLogPath string,
) *WorkLifecycle {
	lc := BeginWorkRestart(t, stderr, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	})
	setWorkLogPath(stderr, lc, agentLogPath, tasks.EventWorkRestart)
	return lc
}

func beginWorkResumeTest(
	t tasks.Task,
	stderr io.Writer,
	agentLogPath string,
) *WorkLifecycle {
	lc := BeginWorkResume(t, stderr)
	setWorkLogPath(stderr, lc, agentLogPath, tasks.EventWorkResume)
	return lc
}

func beginVerifyRestartTest(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, resumeID, agentLogPath string,
) *VerifyLifecycle {
	lc := BeginVerifyRestart(t, stderr, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	})
	setVerifyLogPath(stderr, lc, agentLogPath, tasks.EventVerifyBegin)
	return lc
}

func beginVerifyResumeTest(
	t tasks.Task,
	stderr io.Writer,
	agentLogPath string,
) *VerifyLifecycle {
	lc := BeginVerifyResume(t, stderr)
	setVerifyLogPath(stderr, lc, agentLogPath, tasks.EventVerifyResume)
	return lc
}

func setPlanLogPath(
	stderr io.Writer,
	lc *PlanLifecycle,
	path string,
	ev tasks.Event,
) {
	lc.agentLogPath = path
	lc.task.AgentLogPath = path
	tasks.PersistWarn(stderr, lc.task)
	if path != "" {
		markersHook(tasks.Transition{Event: ev}, lc.task)
	}
}

func setWorkLogPath(
	stderr io.Writer,
	lc *WorkLifecycle,
	path string,
	ev tasks.Event,
) {
	lc.agentLogPath = path
	lc.task.AgentLogPath = path
	tasks.PersistWarn(stderr, lc.task)
	if path != "" {
		markersHook(tasks.Transition{Event: ev}, lc.task)
	}
}

func setVerifyLogPath(
	stderr io.Writer,
	lc *VerifyLifecycle,
	path string,
	ev tasks.Event,
) {
	lc.agentLogPath = path
	lc.task.AgentLogPath = path
	tasks.PersistWarn(stderr, lc.task)
	if path != "" {
		markersHook(tasks.Transition{Event: ev}, lc.task)
	}
}
