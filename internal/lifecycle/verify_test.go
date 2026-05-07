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

// TestTask_BeginVerify_FlipsStatusAndStampsResume pins the begin
// helper: status flips to verifying, the new resume cursor lands on
// the row, work-phase fields are preserved, and stale verify-phase /
// done-at timestamps are cleared.
func TestTask_BeginVerify_FlipsStatusAndStampsResume(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
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
	preWorkBegin := existing.WorkBeginAt
	preWorkEnd := existing.WorkEndAt
	preWorkCursor := existing.WorkResumeSession

	lc := BeginVerify(existing, io.Discard, "cursor", "gpt-5", "fresh-verify-cursor", "")
	lc.Finish(VerifyOutcomeSuccess, nil)

	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusCompleted {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
	if got.VerifyResumeSession != "fresh-verify-cursor" {
		t.Fatalf("VerifyResumeSession = %q", got.VerifyResumeSession)
	}
	if got.WorkResumeSession != preWorkCursor {
		t.Fatalf("WorkResumeSession changed: %q vs %q", got.WorkResumeSession, preWorkCursor)
	}
	if !got.WorkBeginAt.Equal(preWorkBegin) {
		t.Fatalf("WorkBeginAt changed: %v vs %v", got.WorkBeginAt, preWorkBegin)
	}
	if !got.WorkEndAt.Equal(preWorkEnd) {
		t.Fatalf("WorkEndAt changed: %v vs %v", got.WorkEndAt, preWorkEnd)
	}
	if got.VerifyBeginAt.IsZero() || got.VerifyEndAt.IsZero() {
		t.Fatalf("verify timestamps missing: %+v", got)
	}
	if got.DoneAt.IsZero() {
		t.Fatalf("DoneAt should be stamped on completed: %+v", got)
	}
	if got.VerifyModel != "gpt-5" {
		t.Fatalf("VerifyModel = %q", got.VerifyModel)
	}
}

// TestVerifyLifecycle_FinishFailed pins the failed branch.
func TestVerifyLifecycle_FinishFailed(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
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
	lc := BeginVerify(existing, io.Discard, "cursor", "m", "v-cursor", "")
	lc.Finish(VerifyOutcomeNoRetries, nil)
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusFailed {
		t.Fatalf("Status = %q, want failed", got.Status)
	}
	if !got.DoneAt.IsZero() {
		t.Fatalf("DoneAt should remain zero: %v", got.DoneAt)
	}
}

// TestVerifyLifecycle_FinishErrorPath drives the tasks.StatusHelp branch.
func TestVerifyLifecycle_FinishErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
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
	lc := BeginVerify(existing, io.Discard, "cursor", "m", "v-cursor", "")
	lc.Finish(VerifyOutcomeNoRetries, errors.New("boom"))
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help", got.Status)
	}
}

// TestVerifyLifecycle_RecordBackground_StampsPIDAndPath pins the
// happy path of RecordBackground for the verify flow.
func TestVerifyLifecycle_RecordBackground_StampsPIDAndPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
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
	lc := BeginVerify(existing, io.Discard, "cursor", "m", "v-cursor", "")
	lc.RecordBackground(54321, "/tmp/agent.log")
	lc.Finish(VerifyOutcomeSuccess, nil)
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusVerifying {
		t.Fatalf("Status = %q, want verifying", got.Status)
	}
	if got.BackgroundPID != 54321 {
		t.Fatalf("BackgroundPID = %d", got.BackgroundPID)
	}
	if got.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q", got.AgentLogPath)
	}
}

// TestVerifyLifecycle_RecordBackground_ClosedShortCircuit pins the
// second-call no-op for the verify flow.
func TestVerifyLifecycle_RecordBackground_ClosedShortCircuit(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
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
	lc := BeginVerify(existing, io.Discard, "cursor", "m", "v-cursor", "")
	lc.Finish(VerifyOutcomeSuccess, nil)
	lc.RecordBackground(99999, "/tmp/should-not-stick.log")
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusCompleted {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0", got.BackgroundPID)
	}
}

// TestVerifyLifecycle_FinishIdempotent pins the second-Finish no-op.
func TestVerifyLifecycle_FinishIdempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
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
	lc := BeginVerify(existing, io.Discard, "cursor", "m", "v-cursor", "")
	lc.Finish(VerifyOutcomeSuccess, nil)
	lc.Finish(VerifyOutcomeNoRetries, errors.New("ignored"))
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusCompleted {
		t.Fatalf("second finish should be a no-op: %+v", got)
	}
}

// TestBeginVerify_OpenFails forces PutTask's mkdir of the per-task
// directory to fail by replacing `.j/tasks` with a regular file.
// BeginVerify and Finish each emit a warning and continue.
func TestBeginVerify_OpenFails(t *testing.T) {
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
	lc := BeginVerify(tasks.Task{ID: tasks.NewTaskID(), Status: tasks.StatusWorkDone}, &stderr, "cursor", "m", "", "")
	if lc == nil {
		t.Fatal("BeginVerify returned nil")
	}
	lc.Finish(VerifyOutcomeSuccess, nil)
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want tasks warning", stderr.String())
	}
}

// TestBeginVerify_PutTaskErrorWarns drives the put-error branch by
// handing a Task with an empty ID.
func TestBeginVerify_PutTaskErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := BeginVerify(tasks.Task{Status: tasks.StatusWorkDone}, &stderr, "cursor", "m", "", "")
	if lc == nil {
		t.Fatal("BeginVerify returned nil")
	}
	t.Cleanup(func() { lc.Finish(VerifyOutcomeSuccess, nil) })
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestVerifyLifecycle_FinishPutErrorWarns drives the finalize-time
// put warning by handing Finish a task with no ID.
func TestVerifyLifecycle_FinishPutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := &VerifyLifecycle{stderr: &stderr, task: tasks.Task{Status: tasks.StatusVerifying}}
	lc.Finish(VerifyOutcomeSuccess, nil)
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// TestTask_BeginVerifyResume_PreservesLineage pins the resume path's
// invariants: cursor and tool/model untouched, original VerifyBeginAt
// preserved when present, status flipped to verifying.
func TestTask_BeginVerifyResume_PreservesLineage(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	begin := time.Now().UTC().Add(-time.Hour)
	existing := tasks.Task{
		ID:                 tasks.NewTaskID(),
		Status:             tasks.StatusVerifying,
		VerifyTool:         "cursor",
		VerifyModel:        "sonnet-4",
		VerifyResumeSession: "v-cursor",
		VerifyBeginAt:      begin,
	}
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	tasks.PersistWarn(io.Discard, existing)
	lc := BeginVerifyResume(existing, io.Discard, "")
	lc.Finish(VerifyOutcomeSuccess, nil)
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusCompleted {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.VerifyResumeSession != "v-cursor" {
		t.Fatalf("VerifyResumeSession = %q", got.VerifyResumeSession)
	}
	if got.VerifyModel != "sonnet-4" {
		t.Fatalf("VerifyModel = %q", got.VerifyModel)
	}
	if !got.VerifyBeginAt.Equal(begin) {
		t.Fatalf("VerifyBeginAt changed: %v", got.VerifyBeginAt)
	}
}

// TestVerifyLifecycle_MarkersGoToAgentLogNotStderr is the regression
// pin for "phase markers must never reach the user's terminal".
func TestVerifyLifecycle_MarkersGoToAgentLogNotStderr(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
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
	logPath := filepath.Join(t.TempDir(), "agent.log")
	var stderr bytes.Buffer
	t.Cleanup(tasks.ResetHooksForTest)
	tasks.Register(markersHook)
	lc := BeginVerify(existing, &stderr, "cursor", "m", "v-cursor", logPath)
	lc.Finish(VerifyOutcomeSuccess, nil)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read agent.log: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "verify begin") {
		t.Fatalf("agent.log missing verify begin marker: %q", body)
	}
	if !strings.Contains(body, "verify pass") {
		t.Fatalf("agent.log missing verify pass marker: %q", body)
	}
	if strings.Contains(stderr.String(), agentlog.Header("verify_begin")) {
		t.Fatalf("stderr leaked phase marker: %q", stderr.String())
	}
}

// TestVerifyLifecycle_IterationMarkersInAgentLog pins verify_iteration_*
// and verdict markers to agent.log with the same empty-path no-op and
// stderr non-leak semantics as phase markers.
func TestVerifyLifecycle_IterationMarkersInAgentLog(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
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
	logPath := filepath.Join(t.TempDir(), "agent.log")
	var stderr bytes.Buffer
	t.Cleanup(tasks.ResetHooksForTest)
	tasks.Register(markersHook)
	lc := BeginVerify(existing, &stderr, "cursor", "m", "v-cursor", logPath)
	lc.Finish(VerifyOutcomeNoRetries, nil)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read agent.log: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "verify begin") {
		t.Fatalf("agent.log missing verify begin marker: %q", body)
	}
	if !strings.Contains(body, "verify fail") {
		t.Fatalf("agent.log missing verify fail marker: %q", body)
	}
	if strings.Contains(stderr.String(), agentlog.Header("verify_begin")) {
		t.Fatalf("stderr leaked phase marker: %q", stderr.String())
	}
}

// TestBeginVerifyResume_SetsBeginAtWhenZero covers the
// VerifyBeginAt.IsZero() true branch in BeginVerifyResume.
func TestBeginVerifyResume_SetsBeginAtWhenZero(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	task := tasks.Task{
		ID:                  tasks.NewTaskID(),
		Status:              tasks.StatusVerifying,
		VerifyTool:          "cursor",
		VerifyModel:         "m",
		VerifyResumeSession: "v-cursor",
	}
	tasks.PersistWarn(io.Discard, task)
	lc := BeginVerifyResume(task, io.Discard, "")
	if lc.task.VerifyBeginAt.IsZero() {
		t.Fatal("VerifyBeginAt should be stamped when zero at BeginVerifyResume time")
	}
}
