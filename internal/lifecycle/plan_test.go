package lifecycle

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/agentlog"
)

// TestNewPlanTask_RecordsAndFinish drives the planning → plan-done
// happy path: NewPlanTask writes the row at status `planning`, then
// Finish stamps end_at and flips the row to plan-done. The summary
// uses the requirement body (first non-empty line) since it beats
// the file basename.
func TestNewPlanTask_RecordsAndFinish(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	seedPlanApprovalDisabled(t)
	id := tasks.NewTaskID()
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", id, "/tmp/x.md", "# heading\nbody", "plan-cursor", "", "")
	lc.Finish(nil, "# heading\nbody", "## plan", "/tmp/x.md")
	rows := listAllTasks(t)
	if len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("tasks = %+v", rows)
	}
	got := rows[0]
	if got.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.PlanTool != "cursor" || got.PlanModel != "sonnet-4" {
		t.Fatalf("tool/model = %q/%q", got.PlanTool, got.PlanModel)
	}
	if got.PlanResumeSession != "plan-cursor" {
		t.Fatalf("PlanResumeSession = %q", got.PlanResumeSession)
	}
	if got.Summary != "heading" {
		t.Fatalf("Summary = %q, want heading", got.Summary)
	}
	if got.PlanBeginAt.IsZero() || got.PlanEndAt.IsZero() {
		t.Fatalf("timestamps missing: %+v", got)
	}
	if got.PlanEndAt.Before(got.PlanBeginAt) {
		t.Fatalf("end %v before begin %v", got.PlanEndAt, got.PlanBeginAt)
	}
}

// TestPlanLifecycle_Finish_ErrorPath drives the tasks.StatusHelp branch
// when agent.Plan errored.
func TestPlanLifecycle_Finish_ErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewPlanTask(io.Discard, "cursor", "m", tasks.NewTaskID(), "/tmp/x.md", "x", "", "", "")
	lc.Finish(errors.New("boom"), "", "", "/tmp/x.md")
	rows := listAllTasks(t)
	if len(rows) != 1 || rows[0].Status != tasks.StatusHelp {
		t.Fatalf("tasks = %+v, want one help-status task", rows)
	}
}

// TestPlanLifecycle_RecordAgentLog_StampsPath drives the happy path of
// RecordAgentLog: the in-memory task row carries the log path, status
// stays at planning, and a stray Finish call is a silent no-op thanks
// to the closed flag.
func TestPlanLifecycle_RecordAgentLog_StampsPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", tasks.NewTaskID(), "/tmp/x.md", "# heading", "", "", "")
	lc.RecordAgentLog("/tmp/agent.log")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusPlanning {
		t.Fatalf("Status = %q, want planning", got.Status)
	}
	if got.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q", got.AgentLogPath)
	}
}

// TestPlanLifecycle_RecordResumeSession pins the post-run-capture
// path: RecordResumeSession mutates PlanResumeSession in place,
// re-persists the row, and Finish writes the same value through.
// An empty id is a no-op so call sites do not need to gate the
// helper themselves.
func TestPlanLifecycle_RecordResumeSession(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	seedPlanApprovalDisabled(t)
	lc := NewPlanTask(io.Discard, "deepseek", "deepseek-v4-pro",
		tasks.NewTaskID(), "/tmp/x.md", "# heading", "", "", "")
	lc.RecordResumeSession("")
	if got := lc.Task().PlanResumeSession; got != "" {
		t.Fatalf("empty id should not stick: got %q", got)
	}
	lc.RecordResumeSession("captured-id-1")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
	got := listAllTasks(t)[0]
	if got.PlanResumeSession != "captured-id-1" {
		t.Fatalf("PlanResumeSession = %q, want captured-id-1",
			got.PlanResumeSession)
	}
}

// TestPlanLifecycle_RecordAgentLog_ClosedShortCircuit pins the
// second-call no-op: once a lifecycle has been finalised, a
// subsequent RecordAgentLog does nothing.
func TestPlanLifecycle_RecordAgentLog_ClosedShortCircuit(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	seedPlanApprovalDisabled(t)
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", tasks.NewTaskID(), "/tmp/x.md", "# heading", "", "", "")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
	lc.RecordAgentLog("/tmp/should-not-stick.log")
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.AgentLogPath != "" {
		t.Fatalf("AgentLogPath = %q, want empty", got.AgentLogPath)
	}
}

// TestPlanLifecycle_FinishIdempotent pins the closed-flag short
// circuit so a second Finish call is a silent no-op.
func TestPlanLifecycle_FinishIdempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	seedPlanApprovalDisabled(t)
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", tasks.NewTaskID(), "/tmp/x.md", "# heading", "", "", "")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
	lc.Finish(errors.New("boom"), "should not", "change", "anything")
	rows := listAllTasks(t)
	if len(rows) != 1 || rows[0].Status != tasks.StatusPlanDone {
		t.Fatalf("second finish should be a no-op: %+v", rows)
	}
}

// TestPlanLifecycle_FinishPutErrorWarns drives the "tasks put"
// warning branch by feeding a task with no ID.
func TestPlanLifecycle_FinishPutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := &PlanLifecycle{
		stderr: &stderr,
		task:   tasks.Task{Status: tasks.StatusPlanning},
	}
	lc.Finish(nil, "", "", "")
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestNewPlanTask_PutErrorAtBegin pins the put-error branch *inside*
// NewPlanTask: PutTask fails because the task has no ID, the warning
// surfaces, and the begin call still returns a usable lifecycle.
func TestNewPlanTask_PutErrorAtBegin(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := NewPlanTask(&stderr, "cursor", "m", "", "", "", "", "", "")
	if lc == nil {
		t.Fatal("NewPlanTask returned nil")
	}
	t.Cleanup(func() { lc.Finish(nil, "", "", "") })
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestNewPlanTask_OpenFails forces PutTask's mkdir of the per-task
// directory to fail by replacing `.j/tasks` with a regular file.
// Both NewPlanTask and Finish emit a warning and execution continues.
func TestNewPlanTask_OpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	path, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	lc := NewPlanTask(&stderr, "cursor", "m", tasks.NewTaskID(), "", "", "", "", "")
	if lc == nil {
		t.Fatal("NewPlanTask returned nil")
	}
	lc.Finish(nil, "", "", "")
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want some tasks warning", stderr.String())
	}
}

// TestPlanLifecycle_Task returns a value copy of the in-memory task
// row so callers can read it without poking at the unexported field.
func TestPlanLifecycle_Task(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := tasks.NewTaskID()
	lc := NewPlanTask(io.Discard, "cursor", "m", id, "", "", "", "", "")
	if got := lc.Task(); got.ID != id {
		t.Fatalf("Task().ID = %q, want %q", got.ID, id)
	}
}

// TestTask_BeginPlanRestart_PreservesLineage flips an existing plan-done
// row to planning, refreshes the plan resume cursor, and preserves
// the original PlanBeginAt while clearing PlanEndAt / DoneAt.
func TestTask_BeginPlanRestart_PreservesLineage(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	seedPlanApprovalDisabled(t)
	id := seedPlanDoneTask(t, "seeded")
	dbPath, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := tasks.Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	prePlanBegin := existing.PlanBeginAt

	lc := BeginPlanRestart(existing, io.Discard, "cursor", "gpt-5", "fresh-plan-cursor", "")
	lc.Finish(nil, "# refined", "## plan", "/tmp/x.md")
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.PlanResumeSession != "fresh-plan-cursor" {
		t.Fatalf("PlanResumeSession = %q", got.PlanResumeSession)
	}
	if got.PlanModel != "gpt-5" {
		t.Fatalf("PlanModel = %q", got.PlanModel)
	}
	if !got.PlanBeginAt.Equal(prePlanBegin) {
		t.Fatalf("PlanBeginAt = %v, want %v", got.PlanBeginAt, prePlanBegin)
	}
	if got.Summary != "refined" {
		t.Fatalf("Summary = %q", got.Summary)
	}
}

// TestPlanLifecycle_MarkersGoToAgentLogNotStderr is the regression
// pin for "phase markers must never reach the user's terminal". The
// lifecycle is wired with a temp agent.log path; both markers must
// land in that file and stderr must stay clean of the agentlog
// sentinel.
func TestPlanLifecycle_MarkersGoToAgentLogNotStderr(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "agent.log")
	var stderr bytes.Buffer
	t.Cleanup(tasks.ResetHooksForTest)
	tasks.Register(markersHook)
	lc := NewPlanTask(&stderr, "cursor", "m", tasks.NewTaskID(), "/tmp/x.md", "# heading", "", logPath, "")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read agent.log: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "plan begin") {
		t.Fatalf("agent.log missing plan begin marker: %q", body)
	}
	if !strings.Contains(body, "plan ") {
		t.Fatalf("agent.log missing plan end marker: %q", body)
	}
	if strings.Contains(stderr.String(), agentlog.Header("plan_begin")) {
		t.Fatalf("stderr leaked phase marker: %q", stderr.String())
	}
}

// TestBeginPlanRestart_PreservesLinearIssue pins the re-plan
// round-trip for the Linear identifier: a row whose original plan
// stamped a LinearIssue keeps it after BeginPlanRestart mutates the
// row for a re-plan invocation.
func TestBeginPlanRestart_PreservesLinearIssue(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	begin := time.Now().UTC()
	original := tasks.Task{
		ID:          "id-reuse",
		Status:      tasks.StatusPlanDone,
		LinearIssue: "ENG-9",
		PlanBeginAt: begin,
		PlanTool:    "cursor",
		PlanModel:   "sonnet-4",
	}
	lc := BeginPlanRestart(original, io.Discard, "claude", "opus-4", "resume-id", "")
	got := lc.Task()
	if got.LinearIssue != "ENG-9" {
		t.Fatalf("LinearIssue lost across BeginPlanRestart: got %q", got.LinearIssue)
	}
}

func TestBeginPlanExisting_RefreshesPlannerFields(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	task := tasks.Task{
		ID:                tasks.NewTaskID(),
		Status:            tasks.StatusPlanning,
		PlanResumeSession: "old-resume",
	}
	tasks.PersistWarn(io.Discard, task)
	lc := BeginPlanExisting(
		task, io.Discard, "claude", "opus", "new-resume",
		"/tmp/agent.log",
	)
	got := lc.Task()
	if got.PlanTool != "claude" || got.PlanModel != "opus" {
		t.Fatalf("tool/model = %q/%q", got.PlanTool, got.PlanModel)
	}
	if got.PlanResumeSession != "new-resume" {
		t.Fatalf("PlanResumeSession = %q", got.PlanResumeSession)
	}
	if got.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q", got.AgentLogPath)
	}
	if got.PlanBeginAt.IsZero() {
		t.Fatal("PlanBeginAt should be stamped")
	}
}

// TestBeginPlanResume_PreservesSessionAndLineage pins the resume
// branch: BeginPlanResume must NOT overwrite PlanResumeSession,
// must apply EventPlanResume from a plan-pending-approval row, and
// must preserve PlanBeginAt while clearing PlanEndAt / DoneAt.
func TestBeginPlanResume_PreservesSessionAndLineage(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	begin := time.Now().UTC().Add(-time.Hour)
	original := tasks.Task{
		ID:                "id-resume",
		Status:            tasks.StatusPlanPendingApproval,
		LinearIssue:       "ENG-7",
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		PlanResumeSession: "prior-cursor",
		PlanBeginAt:       begin,
		PlanEndAt:         time.Now().UTC(),
	}
	tasks.PersistWarn(io.Discard, original)
	lc := BeginPlanResume(original, io.Discard, "cursor", "sonnet-4", "")
	got := lc.Task()
	if got.PlanResumeSession != "prior-cursor" {
		t.Fatalf("PlanResumeSession = %q, want prior-cursor (resume must not mint)",
			got.PlanResumeSession)
	}
	if got.Status != tasks.StatusPlanning {
		t.Fatalf("Status = %q, want planning", got.Status)
	}
	if !got.PlanBeginAt.Equal(begin) {
		t.Fatalf("PlanBeginAt = %v, want %v", got.PlanBeginAt, begin)
	}
	if !got.PlanEndAt.IsZero() {
		t.Fatalf("PlanEndAt = %v, want zero", got.PlanEndAt)
	}
	if got.LinearIssue != "ENG-7" {
		t.Fatalf("LinearIssue lost across BeginPlanResume: got %q", got.LinearIssue)
	}
}

// TestBeginPlanResume_SetsBeginAtWhenZero covers the
// PlanBeginAt.IsZero() true branch in BeginPlanResume.
func TestBeginPlanResume_SetsBeginAtWhenZero(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	task := tasks.Task{
		ID:                tasks.NewTaskID(),
		Status:            tasks.StatusPlanPendingApproval,
		PlanResumeSession: "prior",
	}
	tasks.PersistWarn(io.Discard, task)
	lc := BeginPlanResume(task, io.Discard, "cursor", "m", "")
	got := lc.Task()
	if got.PlanBeginAt.IsZero() {
		t.Fatal("PlanBeginAt should be stamped when zero at BeginPlanResume time")
	}
}

// TestBeginPlanResume_IllegalTransitionPanics pins the FSM guard:
// resume from a status without a {status, EventPlanResume, planning}
// edge panics with the wrapped event apply error.
func TestBeginPlanResume_IllegalTransitionPanics(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for illegal resume transition")
		}
	}()
	BeginPlanResume(tasks.Task{
		ID:                "id-bad",
		Status:            tasks.StatusWorking,
		PlanResumeSession: "prior",
	}, io.Discard, "cursor", "m", "")
}

// writePlanClarification drops `clarification.md` into
// `<cwd>/.j/tasks/<id>/` so PlanLifecycle.Finish's clarification
// branch fires. Mirrors the layout the planner contract produces.
func writePlanClarification(t *testing.T, id, body string) {
	t.Helper()
	dir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	path := filepath.Join(dir, tasks.ClarificationFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write clarification.md: %v", err)
	}
}

// TestPlanLifecycle_Finish_ClarificationPresent_NoApproval pins the
// foreground clarification branch: a clean run that wrote
// `clarification.md` lands the row in `needs-clarification` instead of
// `plan-done`, stamps PlanEndAt, and leaves Summary alone (refined
// inputs are typically empty there).
func TestPlanLifecycle_Finish_ClarificationPresent_NoApproval(
	t *testing.T,
) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	seedPlanApprovalDisabled(t)
	id := tasks.NewTaskID()
	lc := NewPlanTask(io.Discard, "cursor", "m", id, "/tmp/x.md",
		"# heading\nbody", "", "", "")
	wantSummary := lc.Task().Summary
	writePlanClarification(t, id, "what next?\n")
	lc.Finish(nil, "", "", "/tmp/x.md")
	rows := listAllTasks(t)
	if len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("tasks = %+v", rows)
	}
	got := rows[0]
	if got.Status != tasks.StatusNeedsClarification {
		t.Fatalf("Status = %q, want needs-clarification", got.Status)
	}
	if got.PlanEndAt.IsZero() {
		t.Fatalf("PlanEndAt should be stamped")
	}
	if got.Summary != wantSummary {
		t.Fatalf("Summary = %q, want %q (begin-time, untouched)",
			got.Summary, wantSummary)
	}
}

// TestPlanLifecycle_Finish_ClarificationPresent_ApprovalGate pins
// that clarification.md takes precedence over the approval gate: even
// with `plan_requires_approval=true`, the row lands in
// `needs-clarification` rather than `plan-pending-approval`.
func TestPlanLifecycle_Finish_ClarificationPresent_ApprovalGate(
	t *testing.T,
) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	seedPlanApprovalEnabled(t)
	id := tasks.NewTaskID()
	lc := NewPlanTask(io.Discard, "cursor", "m", id, "/tmp/x.md",
		"# heading\nbody", "", "", "")
	writePlanClarification(t, id, "still ambiguous\n")
	lc.Finish(nil, "", "", "/tmp/x.md")
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusNeedsClarification {
		t.Fatalf("Status = %q, want needs-clarification", got.Status)
	}
}

// TestPlanLifecycle_Finish_ClarificationAbsent_KeepsPlanDoneMatrix
// pins that the plan-done / plan-pending-approval matrix is unchanged
// when clarification.md is absent: approval off → plan-done; approval
// on → plan-pending-approval.
func TestPlanLifecycle_Finish_ClarificationAbsent_KeepsPlanDoneMatrix(
	t *testing.T,
) {
	cases := []struct {
		name     string
		approval bool
		want     tasks.TaskStatus
	}{
		{"approval-off", false, tasks.StatusPlanDone},
		{"approval-on", true, tasks.StatusPlanPendingApproval},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := store.EnsureProject(); err != nil {
				t.Fatalf("EnsureProject: %v", err)
			}
			if c.approval {
				seedPlanApprovalEnabled(t)
			} else {
				seedPlanApprovalDisabled(t)
			}
			id := tasks.NewTaskID()
			lc := NewPlanTask(io.Discard, "cursor", "m", id,
				"/tmp/x.md", "# heading", "", "", "")
			lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
			got := listAllTasks(t)[0]
			if got.Status != c.want {
				t.Fatalf("Status = %q, want %q", got.Status, c.want)
			}
		})
	}
}

// TestPlanLifecycle_Finish_ErrorTrumpsClarification pins the
// precedence rule: a non-nil runErr emits EventPlanError even when
// clarification.md is on disk.
func TestPlanLifecycle_Finish_ErrorTrumpsClarification(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := tasks.NewTaskID()
	lc := NewPlanTask(io.Discard, "cursor", "m", id, "/tmp/x.md",
		"# heading", "", "", "")
	writePlanClarification(t, id, "what next?\n")
	lc.Finish(errors.New("boom"), "", "", "/tmp/x.md")
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help (error precedence)",
			got.Status)
	}
}

// TestBeginPlanRestart_SetsBeginAtWhenZero covers the
// PlanBeginAt.IsZero() true branch in BeginPlanRestart (a task with
// no prior PlanBeginAt).
func TestBeginPlanRestart_SetsBeginAtWhenZero(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	task := tasks.Task{
		ID:        tasks.NewTaskID(),
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "m",
	}
	tasks.PersistWarn(io.Discard, task)
	lc := BeginPlanRestart(task, io.Discard, "cursor", "m", "", "")
	got := lc.Task()
	if got.PlanBeginAt.IsZero() {
		t.Fatal("PlanBeginAt should be stamped when zero at BeginPlanRestart time")
	}
}
