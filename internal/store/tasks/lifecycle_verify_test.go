package tasks

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
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	preWorkBegin := existing.WorkBeginAt
	preWorkEnd := existing.WorkEndAt
	preWorkCursor := existing.WorkResumeSession

	lc := existing.BeginVerify(io.Discard, "cursor", "gpt-5", "fresh-verify-cursor", "")
	lc.Finish(VerifyOutcomeSuccess, nil)

	got := listAllTasks(t)[0]
	if got.Status != StatusCompleted {
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
	if got.InvokedModel != "gpt-5" {
		t.Fatalf("InvokedModel = %q", got.InvokedModel)
	}
}

// TestVerifyLifecycle_FinishNoRetries pins the verify-done branch.
func TestVerifyLifecycle_FinishNoRetries(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := existing.BeginVerify(io.Discard, "cursor", "m", "v-cursor", "")
	lc.Finish(VerifyOutcomeNoRetries, nil)
	got := listAllTasks(t)[0]
	if got.Status != StatusVerifyDone {
		t.Fatalf("Status = %q, want verify-done", got.Status)
	}
	if !got.DoneAt.IsZero() {
		t.Fatalf("DoneAt should remain zero: %v", got.DoneAt)
	}
}

// TestVerifyLifecycle_FinishErrorPath drives the StatusHelp branch.
func TestVerifyLifecycle_FinishErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := existing.BeginVerify(io.Discard, "cursor", "m", "v-cursor", "")
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
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := existing.BeginVerify(io.Discard, "cursor", "m", "v-cursor", "")
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
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := existing.BeginVerify(io.Discard, "cursor", "m", "v-cursor", "")
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
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedWorkDoneTask(t, "x")
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := existing.BeginVerify(io.Discard, "cursor", "m", "v-cursor", "")
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
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	path, err := DefaultDir()
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
	lc := Task{ID: NewTaskID(), Status: StatusWorkDone}.BeginVerify(&stderr, "cursor", "m", "", "")
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
	lc := Task{Status: StatusWorkDone}.BeginVerify(&stderr, "cursor", "m", "", "")
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
	lc := &VerifyLifecycle{stderr: &stderr, task: Task{Status: StatusVerifying}}
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
	existing := Task{
		ID:                 NewTaskID(),
		Status:             StatusVerifyDone,
		InvokedTool:        "cursor",
		InvokedModel:       "sonnet-4",
		VerifyResumeSession: "v-cursor",
		VerifyBeginAt:      begin,
	}
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	PersistWarn(io.Discard, existing)
	lc := existing.BeginVerifyResume(io.Discard, "")
	lc.Finish(VerifyOutcomeSuccess, nil)
	got := listAllTasks(t)[0]
	if got.Status != StatusCompleted {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.VerifyResumeSession != "v-cursor" {
		t.Fatalf("VerifyResumeSession = %q", got.VerifyResumeSession)
	}
	if got.InvokedModel != "sonnet-4" {
		t.Fatalf("InvokedModel = %q", got.InvokedModel)
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
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	logPath := filepath.Join(t.TempDir(), "agent.log")
	var stderr bytes.Buffer
	lc := existing.BeginVerify(&stderr, "cursor", "m", "v-cursor", logPath)
	lc.Finish(VerifyOutcomeSuccess, nil)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read agent.log: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"event":"phase_begin"`) {
		t.Fatalf("agent.log missing phase_begin: %q", body)
	}
	if !strings.Contains(body, `"event":"phase_end"`) {
		t.Fatalf("agent.log missing phase_end: %q", body)
	}
	if strings.Contains(stderr.String(), agentlog.Sentinel) {
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
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	logPath := filepath.Join(t.TempDir(), "agent.log")
	var stderr bytes.Buffer
	lc := existing.BeginVerify(&stderr, "cursor", "m", "v-cursor", logPath)
	lc.IterationBegin(0, 3)
	lc.Verdict(0, "FAIL", "/tmp/findings.md")
	lc.IterationEnd(0, "FAIL")
	lc.Finish(VerifyOutcomeNoRetries, nil)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read agent.log: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"event":"verify_iteration_begin"`) {
		t.Fatalf("agent.log missing verify_iteration_begin: %q", body)
	}
	if !strings.Contains(body, `"event":"verdict"`) {
		t.Fatalf("agent.log missing verdict: %q", body)
	}
	if !strings.Contains(body, `"event":"verify_iteration_end"`) {
		t.Fatalf("agent.log missing verify_iteration_end: %q", body)
	}
	if !strings.Contains(body, id) {
		t.Fatalf("agent.log missing task id %q: %q", id, body)
	}
	if !strings.Contains(body, `"verdict":"FAIL"`) {
		t.Fatalf("agent.log missing FAIL verdict in payload: %q", body)
	}
	if strings.Contains(stderr.String(), agentlog.Sentinel) {
		t.Fatalf("stderr leaked iteration marker: %q", stderr.String())
	}
}
