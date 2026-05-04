package store

import (
	"bytes"
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

	bolt "go.etcd.io/bbolt"
)

// crockfordBase32 is the Crockford base32 alphabet used by ULID
// (uppercase, with I/L/O/U excluded). It is duplicated here on
// purpose: the test asserts the observable contract of NewTaskID
// without importing the ULID package, so a regression that swaps in
// a different alphabet still fails.
const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ptr returns &v; used inline to assemble Task pointer-typed fields in
// fixtures so the call sites stay readable.
func ptr[T any](v T) *T { return &v }

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

// TestWorktreeNameFor covers the slugify + fallback combinations so
// every branch (empty summary falling back to lowercased ULID, pure-
// punctuation summary, empty project, truncation) is exercised.
func TestWorktreeNameFor(t *testing.T) {
	cases := []struct {
		name    string
		project string
		task    Task
		want    string
	}{
		{
			name:    "summary-slugified",
			project: "j",
			task:    Task{ID: "01KQJEHKN55PNJN97SNRPZ6KGB", Summary: "Drop the legacy tasks file"},
			want:    "j-drop-the-legacy-tasks-file",
		},
		{
			name:    "summary-with-punctuation",
			project: "j",
			task:    Task{ID: "01ABC", Summary: "Fix R1: Remove `ErrLegacyTasksFile`!"},
			want:    "j-fix-r1-remove-errlegacytasksfile",
		},
		{
			name:    "empty-summary-falls-back-to-lower-id",
			project: "j",
			task:    Task{ID: "01KQJEHKN55PNJN97SNRPZ6KGB"},
			want:    "j-01kqjehkn55pnjn97snrpz6kgb",
		},
		{
			name:    "pure-punctuation-summary-falls-back-to-id",
			project: "j",
			task:    Task{ID: "01ABC", Summary: "!!! ??? ..."},
			want:    "j-01abc",
		},
		{
			name:    "empty-project-yields-task-only",
			project: "",
			task:    Task{ID: "01ABC", Summary: "hello"},
			want:    "hello",
		},
		{
			name:    "empty-project-and-empty-summary",
			project: "",
			task:    Task{ID: "01ABC"},
			want:    "01abc",
		},
		{
			name:    "empty-project-slug-and-empty-task-slug",
			project: "!!!",
			task:    Task{ID: "!!!"},
			want:    "",
		},
		{
			name:    "both-slugs-empty-after-slugify",
			project: "!!!",
			task:    Task{ID: "", Summary: "???"},
			want:    "",
		},
		{
			name:    "project-only-when-task-slug-empty",
			project: "my-proj",
			task:    Task{ID: "", Summary: ""},
			want:    "my-proj",
		},
		{
			name:    "long-summary-is-clipped",
			project: "j",
			task:    Task{ID: "01ABC", Summary: strings.Repeat("abcdefghij ", 10)},
			want:    "j-abcdefghij-abcdefghij-abcdefghij-abcdefghij-abcd",
		},
		{
			name:    "long-project-is-clipped-too",
			project: strings.Repeat("a", 60),
			task:    Task{ID: "01ABC", Summary: "sum"},
			want:    strings.Repeat("a", 48) + "-sum",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WorktreeNameFor(tc.project, tc.task); got != tc.want {
				t.Fatalf("WorktreeNameFor(%q, %+v) = %q, want %q", tc.project, tc.task, got, tc.want)
			}
		})
	}
}

// TestPutGetTask_WorktreeRoundTrip pins AC for R2: a PutTask ->
// GetTask round trip preserves a non-empty Worktree byte-identically.
func TestPutGetTask_WorktreeRoundTrip(t *testing.T) {
	s := openTaskStore(t)
	in := Task{
		ID:       "id-wt",
		Status:   StatusWorking,
		Summary:  "hello",
		Worktree: "j-my-task",
	}
	if err := s.PutTask(in); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	out, err := s.GetTask("id-wt")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if out.Worktree != "j-my-task" {
		t.Fatalf("Worktree round-trip = %q, want %q", out.Worktree, "j-my-task")
	}
}

// TestProjectName covers the happy path (basename of cwd).
func TestProjectName(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "myproj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	got, err := ProjectName()
	if err != nil {
		t.Fatalf("ProjectName: %v", err)
	}
	if got != "myproj" {
		t.Fatalf("ProjectName = %q, want %q", got, "myproj")
	}
}

func openTaskStore(t *testing.T) *Store {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
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

// TestPutTask_BoltError exercises the bolt update error path: a closed
// DB makes the underlying transaction fail, so PutTask must surface
// that error instead of silently dropping the write.
func TestPutTask_BoltError(t *testing.T) {
	s := openTaskStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.PutTask(Task{ID: "x", Status: StatusPlanDone}); err == nil {
		t.Fatal("PutTask on closed db should error")
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

func TestGetTask_BoltError(t *testing.T) {
	s := openTaskStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.GetTask("x"); err == nil {
		t.Fatal("GetTask on closed db should error")
	}
}

func TestGetTask_DecodeError(t *testing.T) {
	s := openTaskStore(t)
	if err := s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(BucketTasks))
		if err != nil {
			return err
		}
		return b.Put([]byte("bad"), []byte("not-json"))
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := s.GetTask("bad"); err == nil || !strings.Contains(err.Error(), `decode task "bad"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestListTasks_MissingBucket(t *testing.T) {
	s := openTaskStore(t)
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListTasks = %v, want []", got)
	}
}

// TestListTasks_DecodeError plants a non-JSON value under
// BucketTasks so the decode branch in ListTasks fires; the wrapped
// error must surface.
func TestListTasks_DecodeError(t *testing.T) {
	s := openTaskStore(t)
	if err := s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(BucketTasks))
		if err != nil {
			return err
		}
		return b.Put([]byte("bad"), []byte("not-json"))
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := s.ListTasks(); err == nil || !strings.Contains(err.Error(), `decode task "bad"`) {
		t.Fatalf("err = %v", err)
	}
}

// TestListTasks_BoltError exercises the View error branch.
func TestListTasks_BoltError(t *testing.T) {
	s := openTaskStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.ListTasks(); err == nil {
		t.Fatal("ListTasks on closed db should error")
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

func TestSortTasks_ActiveFirstThenByDoneAtDesc(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)

	tasks := []Task{
		{ID: "z-done-old", Status: StatusCompleted, DoneAt: ptr(t1)},
		{ID: "a-done-new", Status: StatusCompleted, DoneAt: ptr(t3)},
		{ID: "m-plandone", Status: StatusPlanDone, DoneAt: ptr(t2)},
		{ID: "active-2", Status: StatusWorking},
		{ID: "active-1", Status: StatusPlanning},
		{ID: "active-3", Status: StatusVerifying},
		{ID: "active-4", Status: StatusHelp},
	}
	SortTasks(tasks)

	wantIDs := []string{
		"active-1", "active-2", "active-3", "active-4",
		"a-done-new", "m-plandone", "z-done-old",
	}
	for i, id := range wantIDs {
		if tasks[i].ID != id {
			t.Fatalf("tasks[%d].ID = %q, want %q (got order: %v)", i, tasks[i].ID, id, idsOf(tasks))
		}
	}
}

// TestSortTasks_FallbackTimes drives every branch in taskFallbackTime
// (DoneAt, VerifyEndAt, WorkEndAt, PlanEndAt, zero).
func TestSortTasks_FallbackTimes(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	t4 := t3.Add(time.Hour)
	tasks := []Task{
		{ID: "plan-only", Status: StatusPlanDone, PlanEndAt: ptr(t1)},
		{ID: "work-end", Status: StatusWorkDone, WorkEndAt: ptr(t3)},
		{ID: "verify-end", Status: StatusVerifyDone, VerifyEndAt: ptr(t4)},
		{ID: "no-time", Status: StatusPlanDone},
		{ID: "done", Status: StatusCompleted, DoneAt: ptr(t2)},
	}
	SortTasks(tasks)
	want := []string{"verify-end", "work-end", "done", "plan-only", "no-time"}
	if got := idsOf(tasks); !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestSortTasks_TieBreakers drives the equal-time path (ID descending
// for inactive) and the equal-active path (ID ascending).
func TestSortTasks_TieBreakers(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tasks := []Task{
		{ID: "inactive-a", Status: StatusCompleted, DoneAt: &at},
		{ID: "inactive-b", Status: StatusCompleted, DoneAt: &at},
		{ID: "active-b", Status: StatusWorking},
		{ID: "active-a", Status: StatusPlanning},
	}
	SortTasks(tasks)
	want := []string{"active-a", "active-b", "inactive-b", "inactive-a"}
	if got := idsOf(tasks); !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSummarizeMarkdown(t *testing.T) {
	cases := []struct{ in, out string }{
		{"", ""},
		{"   \n\n", ""},
		{"# Heading line\nbody", "Heading line"},
		{"### Deep heading", "Deep heading"},
		{"plain first line\nthen heading\n# H", "plain first line"},
	}
	for _, c := range cases {
		if got := SummarizeMarkdown(c.in); got != c.out {
			t.Fatalf("SummarizeMarkdown(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

// TestSummarizeMarkdown_TruncatesRunes pins the rune-aware truncation:
// passing 90 wide-ish unicode runes must yield exactly 80 runes (the
// summaryMaxRunes constant), not 80 bytes.
func TestSummarizeMarkdown_TruncatesRunes(t *testing.T) {
	wide := strings.Repeat("é", 90)
	got := SummarizeMarkdown(wide)
	if want := strings.Repeat("é", summaryMaxRunes); got != want {
		t.Fatalf("len(runes) = %d, want %d", len([]rune(got)), summaryMaxRunes)
	}
}

func idsOf(tasks []Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.ID
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// listAllTasks opens the per-cwd tasks DB, lists every task, and
// closes the store. Used by lifecycle tests to assert what the
// PersistWarn-driven helpers wrote. Returns nil for "no DB yet" so
// the negative-path tests can distinguish "file missing" from a
// real bbolt error.
func listAllTasks(t *testing.T) []Task {
	t.Helper()
	path, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	return got
}

// seedPlanDoneTask seeds a `plan-done` row for the work / verify
// lifecycle tests. The id is returned so callers can look the row
// back up. Use after t.Chdir(t.TempDir()) + EnsureProject().
func seedPlanDoneTask(t *testing.T, summary string) string {
	t.Helper()
	id := NewTaskID()
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	begin := time.Now().UTC().Add(-time.Hour)
	end := begin.Add(time.Minute)
	task := Task{
		ID:               id,
		Status:           StatusPlanDone,
		InvokedTool:      "cursor",
		InvokedModel:     "sonnet-4",
		PlanResumeCursor: "seed-plan-cursor",
		Summary:          summary,
		PlanBeginAt:      &begin,
		PlanEndAt:        &end,
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
	id := NewTaskID()
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planBegin := time.Now().UTC().Add(-2 * time.Hour)
	planEnd := planBegin.Add(time.Minute)
	workBegin := planEnd.Add(time.Minute)
	workEnd := workBegin.Add(time.Minute)
	task := Task{
		ID:               id,
		Status:           StatusWorkDone,
		InvokedTool:      "cursor",
		InvokedModel:     "sonnet-4",
		PlanResumeCursor: "seed-plan-cursor",
		WorkResumeCursor: "seed-work-cursor",
		Summary:          summary,
		PlanBeginAt:      &planBegin,
		PlanEndAt:        &planEnd,
		WorkBeginAt:      &workBegin,
		WorkEndAt:        &workEnd,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}

// TestReadRequirementSidecar_Variants exercises the happy paths plus
// the early-return guards (empty path, empty stem).
func TestReadRequirementSidecar_Variants(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadRequirementSidecar(plan); got != "" {
		t.Fatalf("missing sidecar = %q, want empty", got)
	}
	requirement := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(requirement, []byte("req"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadRequirementSidecar(plan); got != "req" {
		t.Fatalf("present sidecar = %q, want req", got)
	}
	if got := ReadRequirementSidecar(""); got != "" {
		t.Fatalf("empty path = %q", got)
	}
	if got := ReadRequirementSidecar(filepath.Join(dir, ".plan.md")); got != "" {
		t.Fatalf("empty stem = %q", got)
	}
}

// TestReadRequirementSidecar_CandidateEqualsPlan covers the
// "candidate == planPath" guard so a non-conventional plan name does
// not loop reading the same file.
func TestReadRequirementSidecar_CandidateEqualsPlan(t *testing.T) {
	dir := t.TempDir()
	bare := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(bare, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadRequirementSidecar(bare); got != "" {
		t.Fatalf("self-sidecar = %q, want empty", got)
	}
}

// TestSummary_Fallbacks pins the Summary precedence: first non-empty
// markdown line, then file basename, then empty string.
func TestSummary_Fallbacks(t *testing.T) {
	cases := []struct {
		req, target, want string
	}{
		{"# heading\nbody", "/tmp/spec.md", "heading"},
		{"", "/tmp/spec.md", "spec.md"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := Summary(c.req, c.target); got != c.want {
			t.Fatalf("Summary(%q,%q) = %q, want %q", c.req, c.target, got, c.want)
		}
	}
}

// TestPickSource returns whichever of refined-requirements / plan
// markdown yields a non-empty summary, preferring requirements.
func TestPickSource(t *testing.T) {
	cases := []struct {
		req, plan, want string
	}{
		{"# refined", "# pa", "# refined"},
		{"", "# pa", "# pa"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := PickSource(c.req, c.plan); got != c.want {
			t.Fatalf("PickSource(%q,%q) = %q, want %q", c.req, c.plan, got, c.want)
		}
	}
}

// TestFromPlanAndRequirement_Fallbacks pins the work-phase summary
// precedence: requirement first, plan body second, file basename
// last, then empty string.
func TestFromPlanAndRequirement_Fallbacks(t *testing.T) {
	cases := []struct {
		req, plan, planPath, want string
	}{
		{"# req heading\nbody", "## plan", "/tmp/x.plan.md", "req heading"},
		{"", "## plan heading", "/tmp/x.plan.md", "plan heading"},
		{"", "", "/tmp/x.plan.md", "x.plan.md"},
		{"", "", "", ""},
	}
	for _, c := range cases {
		if got := FromPlanAndRequirement(c.req, c.plan, c.planPath); got != c.want {
			t.Fatalf("FromPlanAndRequirement(%q,%q,%q) = %q, want %q", c.req, c.plan, c.planPath, got, c.want)
		}
	}
}

// TestPersistWarn_OpenFailure forces bolt.Open to fail
// by parking a regular file at .j/tasks; a single warning lands on
// stderr and execution returns silently.
func TestPersistWarn_OpenFailure(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jdir, "tasks"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	PersistWarn(&stderr, Task{ID: "x", Status: StatusPlanDone})
	if !strings.Contains(stderr.String(), "warning: tasks") {
		t.Fatalf("stderr = %q, want tasks warning", stderr.String())
	}
}

// TestPersistWarn_PutError opens the layout but feeds PersistWarn a
// task with an empty ID so PutTask errors. The "tasks put" warning
// must surface on stderr.
func TestPersistWarn_PutError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	PersistWarn(&stderr, Task{Status: StatusPlanDone})
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestPersistWarn_RoundTrip pins the happy path: a well-formed task
// is written and a subsequent ListTasks round-trips the row.
func TestPersistWarn_RoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := NewTaskID()
	PersistWarn(io.Discard, Task{ID: id, Status: StatusPlanning, Summary: "hello"})
	got := listAllTasks(t)
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("tasks = %+v, want one row with id %q", got, id)
	}
	if got[0].Summary != "hello" {
		t.Fatalf("Summary = %q, want hello", got[0].Summary)
	}
}

// TestNewPlanTask_RecordsAndFinish drives the planning → plan-done
// happy path: NewPlanTask writes the row at status `planning`, then
// Finish stamps end_at and flips the row to plan-done. The summary
// uses the requirement body (first non-empty line) since it beats
// the file basename.
func TestNewPlanTask_RecordsAndFinish(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := NewTaskID()
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", id, "/tmp/x.md", "# heading\nbody", "plan-cursor")
	lc.Finish(nil, "# heading\nbody", "## plan", "/tmp/x.md")
	tasks := listAllTasks(t)
	if len(tasks) != 1 || tasks[0].ID != id {
		t.Fatalf("tasks = %+v", tasks)
	}
	got := tasks[0]
	if got.Status != StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.InvokedTool != "cursor" || got.InvokedModel != "sonnet-4" {
		t.Fatalf("tool/model = %q/%q", got.InvokedTool, got.InvokedModel)
	}
	if got.PlanResumeCursor != "plan-cursor" {
		t.Fatalf("PlanResumeCursor = %q", got.PlanResumeCursor)
	}
	if got.Summary != "heading" {
		t.Fatalf("Summary = %q, want heading", got.Summary)
	}
	if got.PlanBeginAt == nil || got.PlanEndAt == nil {
		t.Fatalf("timestamps missing: %+v", got)
	}
	if got.PlanEndAt.Before(*got.PlanBeginAt) {
		t.Fatalf("end %v before begin %v", got.PlanEndAt, got.PlanBeginAt)
	}
}

// TestPlanLifecycle_Finish_ErrorPath drives the StatusHelp branch
// when agent.Plan errored.
func TestPlanLifecycle_Finish_ErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	lc := NewPlanTask(io.Discard, "cursor", "m", NewTaskID(), "/tmp/x.md", "x", "")
	lc.Finish(errors.New("boom"), "", "", "/tmp/x.md")
	tasks := listAllTasks(t)
	if len(tasks) != 1 || tasks[0].Status != StatusHelp {
		t.Fatalf("tasks = %+v, want one help-status task", tasks)
	}
}

// TestPlanLifecycle_RecordBackground_StampsPIDAndPath drives the
// happy path of RecordBackground: the in-memory task row carries the
// PID and log path, status stays at planning, and a stray Finish call
// is a silent no-op thanks to the closed flag.
func TestPlanLifecycle_RecordBackground_StampsPIDAndPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.md", "# heading", "")
	lc.RecordBackground(99887, "/tmp/agent.log")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
	got := listAllTasks(t)[0]
	if got.Status != StatusPlanning {
		t.Fatalf("Status = %q, want planning", got.Status)
	}
	if got.BackgroundPID != 99887 {
		t.Fatalf("BackgroundPID = %d", got.BackgroundPID)
	}
	if got.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q", got.AgentLogPath)
	}
}

// TestPlanLifecycle_RecordBackground_ClosedShortCircuit pins the
// second-call no-op: once a lifecycle has been finalised, a
// subsequent RecordBackground does nothing.
func TestPlanLifecycle_RecordBackground_ClosedShortCircuit(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.md", "# heading", "")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
	lc.RecordBackground(11111, "/tmp/should-not-stick.log")
	got := listAllTasks(t)[0]
	if got.Status != StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (closed branch)", got.BackgroundPID)
	}
	if got.AgentLogPath != "" {
		t.Fatalf("AgentLogPath = %q, want empty", got.AgentLogPath)
	}
}

// TestPlanLifecycle_FinishIdempotent pins the closed-flag short
// circuit so a second Finish call is a silent no-op.
func TestPlanLifecycle_FinishIdempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.md", "# heading", "")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
	lc.Finish(errors.New("boom"), "should not", "change", "anything")
	tasks := listAllTasks(t)
	if len(tasks) != 1 || tasks[0].Status != StatusPlanDone {
		t.Fatalf("second finish should be a no-op: %+v", tasks)
	}
}

// TestPlanLifecycle_FinishPutErrorWarns drives the "tasks put"
// warning branch by feeding a task with no ID.
func TestPlanLifecycle_FinishPutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := &PlanLifecycle{stderr: &stderr, task: Task{Status: StatusPlanning}}
	lc.Finish(nil, "", "", "")
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestNewPlanTask_PutErrorAtBegin pins the put-error branch *inside*
// NewPlanTask: PutTask fails because the task has no ID, the warning
// surfaces, and the begin call still returns a usable lifecycle.
func TestNewPlanTask_PutErrorAtBegin(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := NewPlanTask(&stderr, "cursor", "m", "", "", "", "")
	if lc == nil {
		t.Fatal("NewPlanTask returned nil")
	}
	t.Cleanup(func() { lc.Finish(nil, "", "", "") })
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestNewPlanTask_OpenFails forces bolt.Open to
// fail by replacing the post-init list.db file with a directory.
// Both NewPlanTask and Finish emit a warning and execution continues.
func TestNewPlanTask_OpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := DefaultTasksDBPath()
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
	lc := NewPlanTask(&stderr, "cursor", "m", NewTaskID(), "", "", "")
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
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := NewTaskID()
	lc := NewPlanTask(io.Discard, "cursor", "m", id, "", "", "")
	if got := lc.Task(); got.ID != id {
		t.Fatalf("Task().ID = %q, want %q", got.ID, id)
	}
}

// TestTask_BeginPlanReuse_PreservesLineage flips an existing plan-done
// row to planning, refreshes the plan resume cursor, and preserves
// the original PlanBeginAt while clearing PlanEndAt / DoneAt.
func TestTask_BeginPlanReuse_PreservesLineage(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "seeded")
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	prePlanBegin := existing.PlanBeginAt

	lc := existing.BeginPlanReuse(io.Discard, "cursor", "gpt-5", "fresh-plan-cursor")
	lc.Finish(nil, "# refined", "## plan", "/tmp/x.md")
	got := listAllTasks(t)[0]
	if got.Status != StatusPlanDone {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.PlanResumeCursor != "fresh-plan-cursor" {
		t.Fatalf("PlanResumeCursor = %q", got.PlanResumeCursor)
	}
	if got.InvokedModel != "gpt-5" {
		t.Fatalf("InvokedModel = %q", got.InvokedModel)
	}
	if got.PlanBeginAt == nil || !got.PlanBeginAt.Equal(*prePlanBegin) {
		t.Fatalf("PlanBeginAt = %v, want %v", got.PlanBeginAt, prePlanBegin)
	}
	if got.Summary != "refined" {
		t.Fatalf("Summary = %q", got.Summary)
	}
}

// TestNewWorkTask_RecordsRow pins the legacy import write: a fresh
// row at status=working, work fields populated, and no plan fields.
func TestNewWorkTask_RecordsRow(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := NewTaskID()
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", id, "/tmp/spec.plan.md", "# req", "plan body", "work-cursor")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.ID != id || got.Status != StatusWorkDone {
		t.Fatalf("got = %+v", got)
	}
	if got.WorkResumeCursor != "work-cursor" {
		t.Fatalf("WorkResumeCursor = %q", got.WorkResumeCursor)
	}
	if got.PlanResumeCursor != "" {
		t.Fatalf("PlanResumeCursor should stay empty for legacy import: %q", got.PlanResumeCursor)
	}
	if got.WorkBeginAt == nil || got.WorkEndAt == nil {
		t.Fatalf("work timestamps missing: %+v", got)
	}
	if got.DoneAt != nil {
		t.Fatalf("DoneAt should not be set for work-done: %v", got.DoneAt)
	}
}

// TestTask_BeginWorkReuse_PreservesPlanPhase pins the bbolt-sourced
// reuse path: the existing plan-phase fields stay intact.
func TestTask_BeginWorkReuse_PreservesPlanPhase(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "seeded")
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	prePlanBegin := existing.PlanBeginAt
	prePlanEnd := existing.PlanEndAt
	preCursor := existing.PlanResumeCursor

	lc := existing.BeginWorkReuse(io.Discard, "cursor", "gpt-5", "fresh-work-cursor")
	lc.Finish(nil)

	got := listAllTasks(t)[0]
	if got.Status != StatusWorkDone {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.PlanResumeCursor != preCursor {
		t.Fatalf("PlanResumeCursor changed: got %q, want %q", got.PlanResumeCursor, preCursor)
	}
	if got.WorkResumeCursor != "fresh-work-cursor" {
		t.Fatalf("WorkResumeCursor = %q", got.WorkResumeCursor)
	}
	if got.InvokedModel != "gpt-5" {
		t.Fatalf("InvokedModel = %q", got.InvokedModel)
	}
	if got.PlanBeginAt == nil || !got.PlanBeginAt.Equal(*prePlanBegin) {
		t.Fatalf("PlanBeginAt = %v", got.PlanBeginAt)
	}
	if got.PlanEndAt == nil || !got.PlanEndAt.Equal(*prePlanEnd) {
		t.Fatalf("PlanEndAt = %v", got.PlanEndAt)
	}
}

// TestWorkLifecycle_FinishErrorPath drives the StatusHelp branch.
func TestWorkLifecycle_FinishErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "m", NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.Finish(errors.New("boom"))
	got := listAllTasks(t)[0]
	if got.Status != StatusHelp {
		t.Fatalf("Status = %q, want help", got.Status)
	}
	if got.DoneAt != nil {
		t.Fatalf("DoneAt should remain nil on failure: %v", got.DoneAt)
	}
}

// TestWorkLifecycle_RecordBackground_StampsPIDAndPath drives the
// happy path of RecordBackground for the work flow.
func TestWorkLifecycle_RecordBackground_StampsPIDAndPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.RecordBackground(54321, "/tmp/agent.log")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Status != StatusWorking {
		t.Fatalf("Status = %q, want working", got.Status)
	}
	if got.BackgroundPID != 54321 {
		t.Fatalf("BackgroundPID = %d", got.BackgroundPID)
	}
	if got.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q", got.AgentLogPath)
	}
}

// TestWorkLifecycle_RecordBackground_ClosedShortCircuit pins the
// second-call no-op for the work flow.
func TestWorkLifecycle_RecordBackground_ClosedShortCircuit(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.Finish(nil)
	lc.RecordBackground(99999, "/tmp/should-not-stick.log")
	got := listAllTasks(t)[0]
	if got.Status != StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0", got.BackgroundPID)
	}
}

// TestWorkLifecycle_FinishIdempotent pins the second-Finish no-op.
func TestWorkLifecycle_FinishIdempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.Finish(nil)
	lc.Finish(errors.New("ignored"))
	tasks := listAllTasks(t)
	if len(tasks) != 1 || tasks[0].Status != StatusWorkDone {
		t.Fatalf("second finish should be a no-op: %+v", tasks)
	}
}

// TestNewWorkTask_OpenFails forces bolt.Open to
// fail. NewWorkTask and Finish each emit a warning and continue.
func TestNewWorkTask_OpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := DefaultTasksDBPath()
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
	lc := NewWorkTask(&stderr, "cursor", "m", NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	if lc == nil {
		t.Fatal("NewWorkTask returned nil")
	}
	lc.Finish(nil)
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want tasks warning", stderr.String())
	}
}

// TestWorkLifecycle_FinishPutErrorWarns drives the put warning by
// handing Finish a task with no ID.
func TestWorkLifecycle_FinishPutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := &WorkLifecycle{stderr: &stderr, task: Task{Status: StatusWorking}}
	lc.Finish(nil)
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// TestNewWorkTask_MintsWorktreeName pins the worktree slug derivation
// on the legacy-import path: the cwd basename + summary slug.
func TestNewWorkTask_MintsWorktreeName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "m", NewTaskID(), "/tmp/x.plan.md", "# do the thing", "body", "")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Worktree != "myproj-do-the-thing" {
		t.Fatalf("Worktree = %q", got.Worktree)
	}
}

// TestTask_BeginWorkReuse_MintsWorktreeWhenEmpty pins the
// reuse-mint-on-empty branch.
func TestTask_BeginWorkReuse_MintsWorktreeWhenEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "hello world")
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if existing.Worktree != "" {
		t.Fatalf("seed already has worktree %q", existing.Worktree)
	}
	lc := existing.BeginWorkReuse(io.Discard, "cursor", "m", "cursor")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Worktree != "myproj-hello-world" {
		t.Fatalf("Worktree = %q", got.Worktree)
	}
}

// TestTask_BeginWorkReuse_PreservesPreExistingWorktree pins the
// preserve-existing-value branch of fillWorktree.
func TestTask_BeginWorkReuse_PreservesPreExistingWorktree(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "hello")
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	existing.Worktree = "manual-override"
	lc := existing.BeginWorkReuse(io.Discard, "cursor", "m", "cursor")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Worktree != "manual-override" {
		t.Fatalf("Worktree = %q", got.Worktree)
	}
}

// TestTask_BeginWorkResume_LeavesWorktreeAlone pins that resume never
// re-mints Worktree (a pre-R2 task stays empty so the verifier falls
// back to the main checkout).
func TestTask_BeginWorkResume_LeavesWorktreeAlone(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "hello")
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if existing.Worktree != "" {
		t.Fatalf("seed already has worktree %q", existing.Worktree)
	}
	lc := existing.BeginWorkResume(io.Discard)
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Worktree != "" {
		t.Fatalf("Worktree = %q, want empty", got.Worktree)
	}
}

// TestWorkLifecycle_Task returns a value copy of the in-memory task
// row so callers can read freshly-minted Worktree without poking at
// the unexported field.
func TestWorkLifecycle_Task(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "m", NewTaskID(), "/tmp/x.plan.md", "# do the thing", "body", "")
	if got := lc.Task(); got.Worktree != "myproj-do-the-thing" {
		t.Fatalf("Task().Worktree = %q", got.Worktree)
	}
}

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
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
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
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
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
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
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
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
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
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
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
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
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

// TestBeginVerify_OpenFails forces bolt.Open to
// fail. BeginVerify and Finish each emit a warning and continue.
func TestBeginVerify_OpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := DefaultTasksDBPath()
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
