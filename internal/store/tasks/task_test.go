package tasks

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacelions/j/internal/store"
)

func TestTaskStatus_Valid_AllAllowlist(t *testing.T) {
	for _, s := range []TaskStatus{
		StatusPlanning, StatusPlanDone, StatusWorking, StatusWorkDone,
		StatusVerifying, StatusVerifyDone, StatusCompleted, StatusHelp,
	} {
		if !s.Valid() {
			t.Fatalf("Valid(%q) = false, want true", s)
		}
	}
}

func TestTaskStatus_Valid_RejectsUnknown(t *testing.T) {
	for _, s := range []TaskStatus{
		"", "PLANNING", "in-progress", "blocked",
		"planned", "done", // explicitly removed by the new schema
	} {
		if s.Valid() {
			t.Fatalf("Valid(%q) = true, want false", s)
		}
	}
}

func TestNewTaskID_FormatAndUnique(t *testing.T) {
	a := NewTaskID()
	b := NewTaskID()
	if a == b {
		t.Fatalf("NewTaskID returned identical IDs: %q", a)
	}
	for _, id := range []string{a, b} {
		if len(id) != 26 {
			t.Fatalf("len(%q) = %d, want 26", id, len(id))
		}
		for i, r := range id {
			if !strings.ContainsRune(crockfordBase32, r) {
				t.Fatalf("non-Crockford-base32 rune %q at %q[%d]", r, id, i)
			}
		}
	}
	// Monotonic entropy guarantees strict lexicographic ordering
	// even when both calls land in the same millisecond.
	if a >= b {
		t.Fatalf("expected %q < %q (sortable IDs)", a, b)
	}
}

// TestNewTaskID_MonotonicWithinMillisecond exercises the monotonic
// entropy branch deterministically: even when wall-clock time does
// not advance between calls, every minted ID must be strictly
// greater than the previous one.
func TestNewTaskID_MonotonicWithinMillisecond(t *testing.T) {
	const n = 1000
	prev := NewTaskID()
	for i := 1; i < n; i++ {
		id := NewTaskID()
		if id <= prev {
			t.Fatalf("non-monotonic at %d: %q !> %q", i, id, prev)
		}
		prev = id
	}
}

// TestNewTaskID_ConcurrentUnique fires many goroutines through
// NewTaskID at once, asserting both that the sync.Mutex guard around
// the (concurrency-unsafe) monotonic entropy source actually
// serialises callers and that the result set has no duplicates.
func TestNewTaskID_ConcurrentUnique(t *testing.T) {
	const (
		workers = 16
		perCall = 256
	)
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		ids = make(map[string]struct{}, workers*perCall)
	)
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			local := make([]string, perCall)
			for i := 0; i < perCall; i++ {
				local[i] = NewTaskID()
			}
			mu.Lock()
			defer mu.Unlock()
			for _, id := range local {
				if _, dup := ids[id]; dup {
					t.Errorf("duplicate id %q", id)
				}
				ids[id] = struct{}{}
			}
		}()
	}
	wg.Wait()
	if got, want := len(ids), workers*perCall; got != want {
		t.Fatalf("unique id count = %d, want %d", got, want)
	}
}

func TestTask_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	in := Task{
		ID:                 "abc",
		Status:             StatusPlanDone,
		InvokedTool:        "cursor",
		InvokedModel:       "sonnet-4",
		Summary:            "task",
		PlanResumeCursor:   "plan-uuid",
		WorkResumeCursor:   "work-uuid",
		VerifyResumeCursor: "verify-uuid",
		PlanBeginAt:        &now,
		PlanEndAt:          ptr(now.Add(time.Minute)),
		WorkBeginAt:        ptr(now.Add(2 * time.Minute)),
		WorkEndAt:          ptr(now.Add(3 * time.Minute)),
		VerifyBeginAt:      ptr(now.Add(4 * time.Minute)),
		VerifyEndAt:        ptr(now.Add(5 * time.Minute)),
		DoneAt:             ptr(now.Add(6 * time.Minute)),
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{
		`"plan_resume_cursor":"plan-uuid"`,
		`"work_resume_cursor":"work-uuid"`,
		`"verify_resume_cursor":"verify-uuid"`,
		`"status":"plan-done"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected %s in %s", want, data)
		}
	}
	if strings.Contains(string(data), `requirement_markdown`) ||
		strings.Contains(string(data), `plan_markdown`) {
		t.Fatalf("expected body fields removed from JSON, got %s", data)
	}
	var out Task
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ID != in.ID || out.Status != in.Status {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.PlanResumeCursor != "plan-uuid" || out.WorkResumeCursor != "work-uuid" || out.VerifyResumeCursor != "verify-uuid" {
		t.Fatalf("resume cursors round-trip = %+v", out)
	}
	if !out.PlanBeginAt.Equal(now) {
		t.Fatalf("PlanBeginAt = %v, want %v", out.PlanBeginAt, now)
	}
}

// TestTask_JSON_OmitsNilTimestamps pins the omitempty contract on
// pointer-typed time fields (a partially-completed task should not
// serialize fake timestamps).
func TestTask_JSON_OmitsNilTimestamps(t *testing.T) {
	data, err := json.Marshal(Task{ID: "x", Status: StatusPlanning})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(data)
	for _, banned := range []string{"plan_begin_at", "plan_end_at", "work_begin_at", "work_end_at", "verify_begin_at", "verify_end_at", "done_at"} {
		if strings.Contains(got, banned) {
			t.Fatalf("nil timestamp %q should be omitted, got %s", banned, got)
		}
	}
	// Empty Worktree must also be omitted so pre-R2 rows that
	// never gained the field round-trip without the empty key
	// polluting their JSON.
	if strings.Contains(got, "worktree") {
		t.Fatalf("empty worktree should be omitted, got %s", got)
	}
}

func TestPutTask_RoundTrip(t *testing.T) {
	s := openTaskStore(t)
	task := Task{
		ID:               "id-1",
		Status:           StatusPlanDone,
		Summary:          "hello",
		InvokedTool:      "cursor",
		InvokedModel:     "sonnet-4",
		PlanResumeCursor: "plan-1",
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 1 || got[0].ID != "id-1" || got[0].Status != StatusPlanDone {
		t.Fatalf("ListTasks = %+v", got)
	}
	if got[0].PlanResumeCursor != "plan-1" {
		t.Fatalf("PlanResumeCursor lost: %+v", got[0])
	}
}

func TestPutTask_RejectsEmptyID(t *testing.T) {
	s := openTaskStore(t)
	err := s.PutTask(Task{Status: StatusPlanDone})
	if err == nil || !strings.Contains(err.Error(), "task id required") {
		t.Fatalf("err = %v", err)
	}
}

func TestPutTask_RejectsInvalidStatus(t *testing.T) {
	s := openTaskStore(t)
	err := s.PutTask(Task{ID: "x", Status: "blocked"})
	if err == nil || !strings.Contains(err.Error(), `invalid task status "blocked"`) {
		t.Fatalf("err = %v", err)
	}
}

// TestPutTask_MkdirFails forces the per-task mkdir to fail by
// pointing the store at a path whose parent is a regular file. This
// covers the wrapped mkdir error branch in PutTask.
func TestPutTask_MkdirFails(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "tasks-as-file")
	if err := os.WriteFile(parent, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Open(parent)
	if err := s.PutTask(Task{ID: "x", Status: StatusPlanDone}); err == nil {
		t.Fatal("PutTask should error when tasksDir is not a directory")
	}
}

func TestGetTask_RoundTrip(t *testing.T) {
	s := openTaskStore(t)
	in := Task{
		ID:               "id-get",
		Status:           StatusPlanDone,
		Summary:          "hello",
		InvokedTool:      "cursor",
		PlanResumeCursor: "plan-1",
	}
	if err := s.PutTask(in); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	got, err := s.GetTask("id-get")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ID != in.ID || got.Status != in.Status || got.PlanResumeCursor != in.PlanResumeCursor {
		t.Fatalf("GetTask = %+v, want %+v", got, in)
	}
}

func TestGetTask_MissingBucket(t *testing.T) {
	s := openTaskStore(t)
	_, err := s.GetTask("nope")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestGetTask_MissingKey(t *testing.T) {
	s := openTaskStore(t)
	if err := s.PutTask(Task{ID: "id-1", Status: StatusPlanDone}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	_, err := s.GetTask("absent")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

// TestGetTask_DecodeError plants a non-TOML body at the per-task
// path so the toml.Unmarshal branch in GetTask fires; the wrapped
// error must surface.
func TestGetTask_DecodeError(t *testing.T) {
	s := openTaskStore(t)
	taskDir := filepath.Join(s.tasksDir, "bad")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, TaskFileName), []byte("not = valid = toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTask("bad"); err == nil || !strings.Contains(err.Error(), `decode task "bad"`) {
		t.Fatalf("err = %v", err)
	}
}

// TestListTasks_MissingTasksDir pins the contract that a missing
// tasks directory yields an empty slice and a nil error so callers
// can treat "no tasks yet" the same as "no project yet".
func TestListTasks_MissingTasksDir(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "does-not-exist"))
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListTasks = %v, want []", got)
	}
}

// TestListTasks_EmptyTasksDir pins the empty-directory case: the
// per-cwd tasks dir exists but holds no per-task subdirectories.
func TestListTasks_EmptyTasksDir(t *testing.T) {
	s := openTaskStore(t)
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListTasks = %v, want []", got)
	}
}

// TestListTasks_DecodeError plants a non-TOML body at one task's
// path so the toml.Unmarshal branch in ListTasks fires; the wrapped
// error must surface.
func TestListTasks_DecodeError(t *testing.T) {
	s := openTaskStore(t)
	taskDir := filepath.Join(s.tasksDir, "bad")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, TaskFileName), []byte("not = valid = toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListTasks(); err == nil || !strings.Contains(err.Error(), `decode task "bad"`) {
		t.Fatalf("err = %v", err)
	}
}

// TestDeleteTask_RoundTrip pins the happy path: a seeded task can be
// removed and a follow-up GetTask reports it gone via fs.ErrNotExist.
func TestDeleteTask_RoundTrip(t *testing.T) {
	s := openTaskStore(t)
	if err := s.PutTask(Task{ID: "id-del", Status: StatusPlanDone}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.DeleteTask("id-del"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if _, err := s.GetTask("id-del"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("GetTask after delete err = %v, want fs.ErrNotExist", err)
	}
}

// TestDeleteTask_MissingBucket surfaces fs.ErrNotExist when no task
// has ever been written (so the bbolt bucket has not been minted).
func TestDeleteTask_MissingBucket(t *testing.T) {
	s := openTaskStore(t)
	err := s.DeleteTask("nope")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

// TestDeleteTask_MissingKey covers the absent-key branch: the bucket
// exists (some other task is stored) but the requested id is not.
func TestDeleteTask_MissingKey(t *testing.T) {
	s := openTaskStore(t)
	if err := s.PutTask(Task{ID: "present", Status: StatusPlanDone}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	err := s.DeleteTask("absent")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

// TestDeleteTask_BoltError exercises the underlying bolt error path:
// a closed DB makes the Update transaction fail.
func TestDeleteTask_BoltError(t *testing.T) {
	s := openTaskStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.DeleteTask("x"); err == nil {
		t.Fatal("DeleteTask on closed db should error")
	}
}

// TestPutTask_LinearIssueRoundTrip pins the TOML round-trip for the
// linear_issue field: a row stamped with a Linear identifier on
// PutTask reads back identical via GetTask + ListTasks.
func TestPutTask_LinearIssueRoundTrip(t *testing.T) {
	s := openTaskStore(t)
	in := Task{ID: "id-linear", Status: StatusPlanning, LinearIssue: "ENG-123"}
	if err := s.PutTask(in); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	got, err := s.GetTask("id-linear")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.LinearIssue != "ENG-123" {
		t.Fatalf("LinearIssue = %q, want ENG-123", got.LinearIssue)
	}
	rows, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(rows) != 1 || rows[0].LinearIssue != "ENG-123" {
		t.Fatalf("ListTasks = %+v, want one row with LinearIssue ENG-123", rows)
	}
}

// TestPutTask_LinearIssueEmptyOmitted asserts that an empty
// LinearIssue is preserved as the empty string after round-trip and
// the on-disk TOML is decoded back without surprise (existing tasks
// authored before the field land here).
func TestPutTask_LinearIssueEmptyOmitted(t *testing.T) {
	s := openTaskStore(t)
	in := Task{ID: "id-no-linear", Status: StatusPlanDone}
	if err := s.PutTask(in); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	got, err := s.GetTask("id-no-linear")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.LinearIssue != "" {
		t.Fatalf("LinearIssue = %q, want empty", got.LinearIssue)
	}
}

// TestBeginPlanReuse_PreservesLinearIssue pins the re-plan round-trip
// for the Linear identifier: a row whose original plan stamped a
// LinearIssue keeps it after BeginPlanReuse mutates the row for a
// re-plan invocation.
func TestBeginPlanReuse_PreservesLinearIssue(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	begin := time.Now().UTC()
	original := Task{
		ID:           "id-reuse",
		Status:       StatusPlanDone,
		LinearIssue:  "ENG-9",
		PlanBeginAt:  &begin,
		InvokedTool:  "cursor",
		InvokedModel: "sonnet-4",
	}
	lc := original.BeginPlanReuse(io.Discard, "claude", "opus-4", "resume-id", "")
	got := lc.Task()
	if got.LinearIssue != "ENG-9" {
		t.Fatalf("LinearIssue lost across BeginPlanReuse: got %q", got.LinearIssue)
	}
}

// TestDeleteTask_IdempotentSecondCall pins the requirements doc's
// "deletion is permanent" rule by re-running DeleteTask: the second
// call must surface fs.ErrNotExist (not silently succeed) so callers
// can distinguish a real delete from a no-op.
func TestDeleteTask_IdempotentSecondCall(t *testing.T) {
	s := openTaskStore(t)
	if err := s.PutTask(Task{ID: "id-twice", Status: StatusPlanDone}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.DeleteTask("id-twice"); err != nil {
		t.Fatalf("first DeleteTask: %v", err)
	}
	err := s.DeleteTask("id-twice")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("second DeleteTask err = %v, want fs.ErrNotExist", err)
	}
}
