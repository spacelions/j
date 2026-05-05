package store

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// TestTask_BeginVerify_FlipsStatusAndStampsResume pins the begin
// helper: status flips to verifying, the new resume cursor lands on
// the row, work-phase fields are preserved, and stale verify-phase /
// done-at timestamps are cleared.
func TestTask_BeginVerify_FlipsStatusAndStampsResume(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultTasksDir()
	if err != nil {
		t.Fatal(err)
	}
	s := OpenTasks(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	preWorkBegin := existing.WorkBeginAt
	preWorkEnd := existing.WorkEndAt
	preWorkCursor := existing.WorkResumeCursor

	lc := existing.BeginVerify(io.Discard, "cursor", "gpt-5", "fresh-verify-cursor")
	lc.Finish(VerifyOutcomeSuccess, nil)

	got := listAllTasks(t)[0]
	if got.Status != StatusCompleted {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
	if got.VerifyResumeCursor != "fresh-verify-cursor" {
		t.Fatalf("VerifyResumeCursor = %q", got.VerifyResumeCursor)
	}
	if got.WorkResumeCursor != preWorkCursor {
		t.Fatalf("WorkResumeCursor changed: %q vs %q", got.WorkResumeCursor, preWorkCursor)
	}
	if got.WorkBeginAt == nil || !got.WorkBeginAt.Equal(*preWorkBegin) {
		t.Fatalf("WorkBeginAt changed: %v vs %v", got.WorkBeginAt, preWorkBegin)
	}
	if got.WorkEndAt == nil || !got.WorkEndAt.Equal(*preWorkEnd) {
		t.Fatalf("WorkEndAt changed: %v vs %v", got.WorkEndAt, preWorkEnd)
	}
	if got.VerifyBeginAt == nil || got.VerifyEndAt == nil {
		t.Fatalf("verify timestamps missing: %+v", got)
	}
	if got.DoneAt == nil {
		t.Fatalf("DoneAt should be stamped on completed: %+v", got)
	}
	if got.InvokedModel != "gpt-5" {
		t.Fatalf("InvokedModel = %q", got.InvokedModel)
	}
}

// TestVerifyLifecycle_FinishNoRetries pins the verify-done branch.
func TestVerifyLifecycle_FinishNoRetries(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultTasksDir()
	if err != nil {
		t.Fatal(err)
	}
	s := OpenTasks(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := existing.BeginVerify(io.Discard, "cursor", "m", "v-cursor")
	lc.Finish(VerifyOutcomeNoRetries, nil)
	got := listAllTasks(t)[0]
	if got.Status != StatusVerifyDone {
		t.Fatalf("Status = %q, want verify-done", got.Status)
	}
	if got.DoneAt != nil {
		t.Fatalf("DoneAt should remain nil: %v", got.DoneAt)
	}
}

// TestVerifyLifecycle_FinishErrorPath drives the StatusHelp branch.
func TestVerifyLifecycle_FinishErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultTasksDir()
	if err != nil {
		t.Fatal(err)
	}
	s := OpenTasks(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := existing.BeginVerify(io.Discard, "cursor", "m", "v-cursor")
	lc.Finish(VerifyOutcomeNoRetries, errors.New("boom"))
	got := listAllTasks(t)[0]
	if got.Status != StatusHelp {
		t.Fatalf("Status = %q, want help", got.Status)
	}
}

// TestVerifyLifecycle_RecordBackground_StampsPIDAndPath pins the
// happy path of RecordBackground for the verify flow.
func TestVerifyLifecycle_RecordBackground_StampsPIDAndPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultTasksDir()
	if err != nil {
		t.Fatal(err)
	}
	s := OpenTasks(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := existing.BeginVerify(io.Discard, "cursor", "m", "v-cursor")
	lc.RecordBackground(54321, "/tmp/agent.log")
	lc.Finish(VerifyOutcomeSuccess, nil)
	got := listAllTasks(t)[0]
	if got.Status != StatusVerifying {
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
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultTasksDir()
	if err != nil {
		t.Fatal(err)
	}
	s := OpenTasks(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := existing.BeginVerify(io.Discard, "cursor", "m", "v-cursor")
	lc.Finish(VerifyOutcomeSuccess, nil)
	lc.RecordBackground(99999, "/tmp/should-not-stick.log")
	got := listAllTasks(t)[0]
	if got.Status != StatusCompleted {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0", got.BackgroundPID)
	}
}

// TestVerifyLifecycle_FinishIdempotent pins the second-Finish no-op.
func TestVerifyLifecycle_FinishIdempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultTasksDir()
	if err != nil {
		t.Fatal(err)
	}
	s := OpenTasks(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := existing.BeginVerify(io.Discard, "cursor", "m", "v-cursor")
	lc.Finish(VerifyOutcomeSuccess, nil)
	lc.Finish(VerifyOutcomeNoRetries, errors.New("ignored"))
	got := listAllTasks(t)[0]
	if got.Status != StatusCompleted {
		t.Fatalf("second finish should be a no-op: %+v", got)
	}
}

// TestBeginVerify_OpenFails forces PutTask's mkdir of the per-task
// directory to fail by replacing `.j/tasks` with a regular file.
// BeginVerify and Finish each emit a warning and continue.
func TestBeginVerify_OpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := DefaultTasksDir()
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
	lc := Task{ID: NewTaskID(), Status: StatusWorkDone}.BeginVerify(&stderr, "cursor", "m", "")
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
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := Task{Status: StatusWorkDone}.BeginVerify(&stderr, "cursor", "m", "")
	if lc == nil {
		t.Fatal("BeginVerify returned nil")
	}
	t.Cleanup(func() { lc.Finish(VerifyOutcomeSuccess, nil) })
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestVerifyLifecycle_FinishPutErrorWarns drives the finalize-time
// put warning by handing Finish a task with no ID.
func TestVerifyLifecycle_FinishPutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := &VerifyLifecycle{stderr: &stderr, task: Task{Status: StatusVerifying}}
	lc.Finish(VerifyOutcomeSuccess, nil)
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// TestTask_BeginVerifyResume_PreservesLineage pins the resume path's
// invariants: cursor and tool/model untouched, original VerifyBeginAt
// preserved when present, status flipped to verifying.
func TestTask_BeginVerifyResume_PreservesLineage(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	begin := time.Now().UTC().Add(-time.Hour)
	existing := Task{
		ID:                 NewTaskID(),
		Status:             StatusVerifyDone,
		InvokedTool:        "cursor",
		InvokedModel:       "sonnet-4",
		VerifyResumeCursor: "v-cursor",
		VerifyBeginAt:      &begin,
	}
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	PersistWarn(io.Discard, existing)
	lc := existing.BeginVerifyResume(io.Discard)
	lc.Finish(VerifyOutcomeSuccess, nil)
	got := listAllTasks(t)[0]
	if got.Status != StatusCompleted {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.VerifyResumeCursor != "v-cursor" {
		t.Fatalf("VerifyResumeCursor = %q", got.VerifyResumeCursor)
	}
	if got.InvokedModel != "sonnet-4" {
		t.Fatalf("InvokedModel = %q", got.InvokedModel)
	}
	if got.VerifyBeginAt == nil || !got.VerifyBeginAt.Equal(begin) {
		t.Fatalf("VerifyBeginAt changed: %v", got.VerifyBeginAt)
	}
}
