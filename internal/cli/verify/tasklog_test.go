package verify

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// TestBeginVerifyTask_FlipsStatusAndStampsResume pins the begin
// helper: the existing row's status flips to verifying, the new
// resume cursor lands on the row, the original work-phase fields
// are preserved, and stale verify-phase / done-at timestamps are
// cleared.
func TestBeginVerifyTask_FlipsStatusAndStampsResume(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	preWorkBegin := existing.WorkBeginAt
	preWorkEnd := existing.WorkEndAt
	preWorkCursor := existing.WorkResumeCursor

	lc := beginVerifyTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "gpt-5", existing, "fresh-verify-cursor")
	lc.finishVerify(outcomeSuccess, nil)

	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].ID != id {
		t.Fatalf("tasks = %+v", tasks)
	}
	got := tasks[0]
	if got.Status != store.StatusCompleted {
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
		t.Fatalf("InvokedModel = %q, want gpt-5", got.InvokedModel)
	}
}

// TestFinishVerify_NoRetries pins the verify-done branch.
func TestFinishVerify_NoRetries(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := beginVerifyTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "m", existing, "v-cursor")
	lc.finishVerify(outcomeNoRetries, nil)
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusVerifyDone {
		t.Fatalf("Status = %q, want verify-done", tasks[0].Status)
	}
	if tasks[0].DoneAt != nil {
		t.Fatalf("DoneAt should remain nil on verify-done: %v", tasks[0].DoneAt)
	}
}

// TestFinishVerify_ErrorPath drives the StatusHelp branch.
func TestFinishVerify_ErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := beginVerifyTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "m", existing, "v-cursor")
	lc.finishVerify(outcomeNoRetries, errors.New("boom"))
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusHelp {
		t.Fatalf("Status = %q, want help", tasks[0].Status)
	}
}

// TestRecordBackground_StampsPIDAndPath pins the happy path of
// recordBackground for the verify flow.
func TestRecordBackground_StampsPIDAndPath(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := beginVerifyTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "m", existing, "v-cursor")
	lc.recordBackground(54321, "/tmp/agent.log")
	lc.finishVerify(outcomeSuccess, nil)
	got := readTasks(t)[0]
	if got.Status != store.StatusVerifying {
		t.Fatalf("Status = %q, want verifying (background sticks)", got.Status)
	}
	if got.BackgroundPID != 54321 {
		t.Fatalf("BackgroundPID = %d", got.BackgroundPID)
	}
	if got.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q", got.AgentLogPath)
	}
}

// TestRecordBackground_ClosedShortCircuit pins the second-call
// no-op for `j verify`.
func TestRecordBackground_ClosedShortCircuit(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := beginVerifyTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "m", existing, "v-cursor")
	lc.finishVerify(outcomeSuccess, nil)
	lc.recordBackground(99999, "/tmp/should-not-stick.log")
	got := readTasks(t)[0]
	if got.Status != store.StatusCompleted {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (closed branch)", got.BackgroundPID)
	}
}

// TestFinishVerify_Idempotent locks down the closed-flag short
// circuit on a second finishVerify call.
func TestFinishVerify_Idempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lc := beginVerifyTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "m", existing, "v-cursor")
	lc.finishVerify(outcomeSuccess, nil)
	lc.finishVerify(outcomeNoRetries, errors.New("ignored"))
	got := readTasks(t)[0]
	if got.Status != store.StatusCompleted {
		t.Fatalf("second finish should be a no-op: %+v", got)
	}
}

// TestOpenLifecycle_OpenTaskLogFails forces openTaskLog to return
// ok=false so the lifecycle helpers warn instead of panicking.
func TestOpenLifecycle_OpenTaskLogFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	lc := beginVerifyTask(Options{Stderr: &stderr}, &scriptedAgent{name: "cursor"}, "m", store.Task{ID: store.NewTaskID(), Status: store.StatusWorkDone}, "")
	if lc == nil {
		t.Fatal("beginVerifyTask returned nil lifecycle")
	}
	lc.finishVerify(outcomeSuccess, nil)
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want tasks warning", stderr.String())
	}
}

// TestOpenLifecycle_PutTaskErrorWarns drives the put-error branch
// inside openLifecycle by handing it a Task with an empty ID.
func TestOpenLifecycle_PutTaskErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	var stderr bytes.Buffer
	lc := beginVerifyTask(Options{Stderr: &stderr}, &scriptedAgent{name: "cursor"}, "m", store.Task{Status: store.StatusWorkDone}, "")
	if lc == nil {
		t.Fatal("beginVerifyTask returned nil lifecycle")
	}
	t.Cleanup(func() { lc.finishVerify(outcomeSuccess, nil) })
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestFinishVerify_PutErrorWarns drives the finalize-time put
// warning by handing finishVerify a task with no ID.
func TestFinishVerify_PutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	var stderr bytes.Buffer
	lc := &verifyLifecycle{stderr: &stderr, task: store.Task{Status: store.StatusVerifying}}
	lc.finishVerify(outcomeSuccess, nil)
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
