package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

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
		// 16 hex + "-" + 8 hex = 25 chars total.
		if len(id) != 25 {
			t.Fatalf("len(%q) = %d, want 25", id, len(id))
		}
		if id[16] != '-' {
			t.Fatalf("separator missing in %q", id)
		}
	}
	// Counter advances monotonically inside the same nanosecond
	// budget, so `b` is never <= `a` lexicographically.
	if a >= b {
		t.Fatalf("expected %q < %q (sortable IDs)", a, b)
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
