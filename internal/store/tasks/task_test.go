package tasks

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func TestTaskStatus_Valid_AllAllowlist(t *testing.T) {
	for _, s := range []TaskStatus{
		StatusPlanning, StatusPlanDone, StatusWorking, StatusWorkDone,
		StatusVerifying, StatusFailed, StatusCompleted, StatusHelp,
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
	for range workers {
		go func() {
			defer wg.Done()
			local := make([]string, perCall)
			for i := range perCall {
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
		ID:                  "abc",
		Status:              StatusPlanDone,
		PlanTool:            "cursor",
		PlanModel:           "sonnet-4",
		Summary:             "task",
		PlanResumeSession:   "plan-uuid",
		WorkResumeSession:   "work-uuid",
		VerifyResumeSession: "verify-uuid",
		PlanBeginAt:         now,
		PlanEndAt:           now.Add(time.Minute),
		WorkBeginAt:         now.Add(2 * time.Minute),
		WorkEndAt:           now.Add(3 * time.Minute),
		VerifyBeginAt:       now.Add(4 * time.Minute),
		VerifyEndAt:         now.Add(5 * time.Minute),
		DoneAt:              now.Add(6 * time.Minute),
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{
		`"PlanResumeSession":"plan-uuid"`,
		`"WorkResumeSession":"work-uuid"`,
		`"VerifyResumeSession":"verify-uuid"`,
		`"Status":"plan-done"`,
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
	if out.PlanResumeSession != "plan-uuid" || out.WorkResumeSession != "work-uuid" || out.VerifyResumeSession != "verify-uuid" {
		t.Fatalf("resume sessions round-trip = %+v", out)
	}
	if !out.PlanBeginAt.Equal(now) {
		t.Fatalf("PlanBeginAt = %v, want %v", out.PlanBeginAt, now)
	}
}

// TestTask_JSON_RoundTripsBasicFields confirms that JSON encoding uses
// PascalCase field names (no json struct tags) and the round-trip
// preserves ID, Status, and resume sessions.
func TestTask_JSON_RoundTripsBasicFields(t *testing.T) {
	in := Task{ID: "x", Status: StatusPlanning, PlanResumeSession: "abc"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out Task
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ID != "x" || out.Status != StatusPlanning {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.PlanResumeSession != "abc" {
		t.Fatalf("PlanResumeSession round-trip = %q", out.PlanResumeSession)
	}
}

// TestTask_TOML_OmitsZeroFields verifies that optional fields with zero/empty
// values are not emitted by toml.Marshal. Time fields are excluded from this
// check because pelletier v2.3.0 does not honour omitempty on time.Time
// (documented in wire_test.go).
func TestTask_TOML_OmitsZeroFields(t *testing.T) {
	task := Task{ID: "x", Status: StatusPlanning}
	data, err := toml.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)
	for _, key := range []string{
		"plan_tool",
		"plan_model",
		"work_tool",
		"work_model",
		"verify_tool",
		"verify_model",
		"worktree",
		"plan_resume_session",
		"work_resume_session",
		"verify_resume_session",
		"agent_log_path",
		"linear_issue",
	} {
		if strings.Contains(s, key) {
			t.Errorf("TOML output contains %q but should be omitted:\n%s", key, s)
		}
	}
}

// TestTask_TOML_BackwardCompatible_ZeroSentinels confirms that legacy
// task.toml files written before this cleanup — which contain zero-time
// sentinels like `plan_begin_at = 0001-01-01T00:00:00Z` and empty strings
// like `worktree = ”` — decode cleanly to zero/empty Go values. The old
// `*_resume_cursor` keys are silently dropped (by design — no compat shim).
func TestTask_TOML_BackwardCompatible_ZeroSentinels(t *testing.T) {
	legacy := `
id = "abc"
status = "plan-done"
invoked_tool = "cursor"
invoked_model = "sonnet-4"
worktree = ''
summary = "old task"
plan_begin_at = 0001-01-01T00:00:00Z
plan_end_at = 0001-01-01T00:00:00Z
work_begin_at = 0001-01-01T00:00:00Z
work_end_at = 0001-01-01T00:00:00Z
verify_begin_at = 0001-01-01T00:00:00Z
verify_end_at = 0001-01-01T00:00:00Z
done_at = 0001-01-01T00:00:00Z
plan_resume_cursor = "old-cursor-value"
`
	var task Task
	if err := toml.Unmarshal([]byte(legacy), &task); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if task.ID != "abc" || task.Status != StatusPlanDone {
		t.Fatalf("basic fields: %+v", task)
	}
	if task.Worktree != "" {
		t.Errorf("Worktree = %q, want empty", task.Worktree)
	}
	if !task.PlanBeginAt.IsZero() {
		t.Errorf("PlanBeginAt = %v, want zero", task.PlanBeginAt)
	}
	if !task.PlanEndAt.IsZero() {
		t.Errorf("PlanEndAt = %v, want zero", task.PlanEndAt)
	}
	// The old plan_resume_cursor key is silently dropped — no compat shim.
	if task.PlanResumeSession != "" {
		t.Errorf("PlanResumeSession = %q, want empty (old key silently dropped)", task.PlanResumeSession)
	}
}

func TestPutTask_RoundTrip(t *testing.T) {
	s := openTaskStore(t)
	task := Task{
		ID:                "id-1",
		Status:            StatusPlanDone,
		Summary:           "hello",
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		PlanResumeSession: "plan-1",
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
	if got[0].PlanResumeSession != "plan-1" {
		t.Fatalf("PlanResumeSession lost: %+v", got[0])
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
		ID:                "id-get",
		Status:            StatusPlanDone,
		Summary:           "hello",
		PlanTool:          "cursor",
		PlanResumeSession: "plan-1",
	}
	if err := s.PutTask(in); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	got, err := s.GetTask("id-get")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ID != in.ID || got.Status != in.Status || got.PlanResumeSession != in.PlanResumeSession {
		t.Fatalf("GetTask = %+v, want %+v", got, in)
	}
}

// TestGetTask_ReadFilePermissionError plants a task.toml with mode 000 so
// os.ReadFile returns a non-ErrNotExist error, covering the read-error branch.
func TestGetTask_ReadFilePermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	s := openTaskStore(t)
	if err := s.PutTask(Task{ID: "perm-get", Status: StatusPlanDone}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	tomlPath := filepath.Join(s.tasksDir, "perm-get", TaskFileName)
	if err := os.Chmod(tomlPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tomlPath, 0o644) })
	_, err := s.GetTask("perm-get")
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want non-ErrNotExist read error", err)
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

// TestDeleteTask_RemoveError creates a task then makes its parent directory
// read-only so os.Remove can't unlink task.toml, covering the remove-error branch.
func TestDeleteTask_RemoveError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	s := openTaskStore(t)
	if err := s.PutTask(Task{ID: "rm-fail", Status: StatusPlanDone}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	taskDir := filepath.Join(s.tasksDir, "rm-fail")
	if err := os.Chmod(taskDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(taskDir, 0o755) })
	if err := s.DeleteTask("rm-fail"); err == nil {
		t.Fatal("DeleteTask should fail when task dir is read-only")
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

// TestPutTask_NonTasksStore covers the s.tasksDir == "" guard.
func TestPutTask_NonTasksStore(t *testing.T) {
	s := Open("")
	err := s.PutTask(Task{ID: "x", Status: StatusPlanDone})
	if err == nil || !strings.Contains(err.Error(), "non-tasks store") {
		t.Fatalf("err = %v, want non-tasks-store error", err)
	}
}

// TestGetTask_NonTasksStore covers the s.tasksDir == "" guard.
func TestGetTask_NonTasksStore(t *testing.T) {
	s := Open("")
	_, err := s.GetTask("x")
	if err == nil || !strings.Contains(err.Error(), "non-tasks store") {
		t.Fatalf("err = %v, want non-tasks-store error", err)
	}
}

// TestDeleteTask_NonTasksStore covers the s.tasksDir == "" guard.
func TestDeleteTask_NonTasksStore(t *testing.T) {
	s := Open("")
	err := s.DeleteTask("x")
	if err == nil || !strings.Contains(err.Error(), "non-tasks store") {
		t.Fatalf("err = %v, want non-tasks-store error", err)
	}
}

// TestListTasks_NonTasksStore covers the s.tasksDir == "" guard.
func TestListTasks_NonTasksStore(t *testing.T) {
	s := Open("")
	_, err := s.ListTasks()
	if err == nil || !strings.Contains(err.Error(), "non-tasks store") {
		t.Fatalf("err = %v, want non-tasks-store error", err)
	}
}

// TestListTasks_SkipsNonDirEntries places a regular file directly
// inside the tasks directory and confirms ListTasks silently skips it.
func TestListTasks_SkipsNonDirEntries(t *testing.T) {
	s := openTaskStore(t)
	// Plant a stray file at the top level of the tasks dir.
	if err := os.WriteFile(filepath.Join(s.tasksDir, "stray.txt"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListTasks = %v, want []", got)
	}
}

// TestListTasks_SortOrder seeds two tasks out of ULID order and
// confirms ListTasks returns them in ascending ID order (hitting
// the sort comparator).
func TestListTasks_SortOrder(t *testing.T) {
	s := openTaskStore(t)
	first := Task{ID: "01AAAA0000000000000000001", Status: StatusPlanDone}
	second := Task{ID: "01AAAA0000000000000000002", Status: StatusPlanDone}
	// Insert in reverse order to exercise the sort.
	if err := s.PutTask(second); err != nil {
		t.Fatalf("PutTask second: %v", err)
	}
	if err := s.PutTask(first); err != nil {
		t.Fatalf("PutTask first: %v", err)
	}
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListTasks = %v, want 2 tasks", got)
	}
	if got[0].ID != first.ID || got[1].ID != second.ID {
		t.Fatalf("sort order wrong: got %q %q", got[0].ID, got[1].ID)
	}
}

// TestListTasks_ReadFilePermissionError plants a task directory with a
// task.toml that has mode 000, so os.ReadFile returns a non-ErrNotExist error.
func TestListTasks_ReadFilePermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	s := openTaskStore(t)
	if err := s.PutTask(Task{ID: "perm-list", Status: StatusPlanDone}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	tomlPath := filepath.Join(s.tasksDir, "perm-list", TaskFileName)
	if err := os.Chmod(tomlPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tomlPath, 0o644) })
	_, err := s.ListTasks()
	if err == nil || !strings.Contains(err.Error(), "store: read") {
		t.Fatalf("err = %v, want store-read error", err)
	}
}

// TestListTasks_SubdirWithNoTaskToml creates a bare subdirectory (no
// task.toml inside) so the ErrNotExist → continue path in ListTasks fires.
func TestListTasks_SubdirWithNoTaskToml(t *testing.T) {
	s := openTaskStore(t)
	// Create a subdirectory with no task.toml inside.
	if err := os.MkdirAll(filepath.Join(s.tasksDir, "no-toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListTasks = %v, want [] (subdir without task.toml skipped)", got)
	}
}

// TestListTasks_ReadDirError replaces the tasks directory with a regular file
// so os.ReadDir returns a non-ErrNotExist error, covering the readdir error branch.
func TestListTasks_ReadDirError(t *testing.T) {
	s := openTaskStore(t)
	// Replace the tasks directory with a regular file to make ReadDir fail.
	if err := os.RemoveAll(s.tasksDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.tasksDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.ListTasks()
	if err == nil || !strings.Contains(err.Error(), "readdir") {
		t.Fatalf("err = %v, want readdir error", err)
	}
}

// TestDisplayToolModel pins the status-based dispatch table: each status
// returns the correct phase pair, and StatusHelp falls back through
// verify → work → plan in priority order.
func TestDisplayToolModel(t *testing.T) {
	base := Task{
		PlanTool:    "ptool",
		PlanModel:   "pmodel",
		WorkTool:    "wtool",
		WorkModel:   "wmodel",
		VerifyTool:  "vtool",
		VerifyModel: "vmodel",
	}
	cases := []struct {
		status    TaskStatus
		wantTool  string
		wantModel string
	}{
		{StatusPlanning, "ptool", "pmodel"},
		{StatusPlanPendingApproval, "ptool", "pmodel"},
		{StatusPlanDone, "ptool", "pmodel"},
		{StatusWorking, "wtool", "wmodel"},
		{StatusWorkDone, "wtool", "wmodel"},
		{StatusVerifying, "vtool", "vmodel"},
		{StatusFailed, "vtool", "vmodel"},
		{StatusCompleted, "vtool", "vmodel"},
		{StatusHelp, "vtool", "vmodel"},
		{StatusNeedsClarification, "vtool", "vmodel"},
		{"unknown", "ptool", "pmodel"},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			task := base
			task.Status = tc.status
			tool, model := task.DisplayToolModel()
			if tool != tc.wantTool || model != tc.wantModel {
				t.Fatalf("DisplayToolModel() = %q/%q, want %q/%q", tool, model, tc.wantTool, tc.wantModel)
			}
		})
	}
	// StatusHelp falls back: no verify → work
	t.Run("help_fallback_to_work", func(t *testing.T) {
		task := Task{Status: StatusHelp, WorkTool: "wtool", WorkModel: "wmodel"}
		tool, model := task.DisplayToolModel()
		if tool != "wtool" || model != "wmodel" {
			t.Fatalf("DisplayToolModel() = %q/%q, want wtool/wmodel", tool, model)
		}
	})
	// StatusHelp falls back: no verify, no work → plan
	t.Run("help_fallback_to_plan", func(t *testing.T) {
		task := Task{Status: StatusHelp, PlanTool: "ptool", PlanModel: "pmodel"}
		tool, model := task.DisplayToolModel()
		if tool != "ptool" || model != "pmodel" {
			t.Fatalf("DisplayToolModel() = %q/%q, want ptool/pmodel", tool, model)
		}
	})
}
