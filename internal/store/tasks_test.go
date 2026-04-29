package store

import (
	"encoding/json"
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
		StatusPlanning, StatusPlanned, StatusWorking,
		StatusVerifying, StatusDone, StatusHelp,
	} {
		if !s.Valid() {
			t.Fatalf("Valid(%q) = false, want true", s)
		}
	}
}

func TestTaskStatus_Valid_RejectsUnknown(t *testing.T) {
	for _, s := range []TaskStatus{"", "PLANNING", "in-progress", "blocked"} {
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
	plan := "## plan body"
	in := Task{
		ID:                  "abc",
		RequirementMarkdown: "# task\nbody",
		PlanMarkdown:        &plan,
		Status:              StatusPlanned,
		InvokedTool:         "cursor",
		InvokedModel:        "sonnet-4",
		ResumeCursor:        "/work/dir",
		Summary:             "task",
		PlanBeginAt:         &now,
		PlanEndAt:           ptr(now.Add(time.Minute)),
		WorkBeginAt:         ptr(now.Add(2 * time.Minute)),
		WorkEndAt:           ptr(now.Add(3 * time.Minute)),
		VerifyBeginAt:       ptr(now.Add(4 * time.Minute)),
		VerifyEndAt:         ptr(now.Add(5 * time.Minute)),
		DoneAt:              ptr(now.Add(6 * time.Minute)),
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"plan_markdown":"## plan body"`) {
		t.Fatalf("expected plan_markdown to round-trip non-null, got %s", data)
	}
	var out Task
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ID != in.ID || out.Status != in.Status {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.PlanMarkdown == nil || *out.PlanMarkdown != plan {
		t.Fatalf("PlanMarkdown round-trip = %v", out.PlanMarkdown)
	}
	if !out.PlanBeginAt.Equal(now) {
		t.Fatalf("PlanBeginAt = %v, want %v", out.PlanBeginAt, now)
	}
}

// TestTask_JSON_NullablePlanMarkdown pins that an unset PlanMarkdown
// serializes as JSON null (per plan: "JSON-null when unknown") and a
// missing optional timestamp is omitted entirely.
func TestTask_JSON_NullablePlanMarkdown(t *testing.T) {
	data, err := json.Marshal(Task{ID: "x", Status: StatusPlanning})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"plan_markdown":null`) {
		t.Fatalf("expected plan_markdown null, got %s", got)
	}
	if strings.Contains(got, "plan_begin_at") {
		t.Fatalf("nil timestamps should be omitted, got %s", got)
	}
}

func openTaskStore(t *testing.T) *Store {
	t.Helper()
	t.Chdir(t.TempDir())
	path, err := DefaultTasksPath()
	if err != nil {
		t.Fatalf("DefaultTasksPath: %v", err)
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
	plan := "plan"
	task := Task{
		ID:           "id-1",
		Status:       StatusPlanned,
		Summary:      "hello",
		InvokedTool:  "cursor",
		InvokedModel: "sonnet-4",
		PlanMarkdown: &plan,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 1 || got[0].ID != "id-1" || got[0].Status != StatusPlanned {
		t.Fatalf("ListTasks = %+v", got)
	}
	if got[0].PlanMarkdown == nil || *got[0].PlanMarkdown != plan {
		t.Fatalf("PlanMarkdown lost: %+v", got[0].PlanMarkdown)
	}
}

func TestPutTask_RejectsEmptyID(t *testing.T) {
	s := openTaskStore(t)
	err := s.PutTask(Task{Status: StatusPlanned})
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
	if err := s.PutTask(Task{ID: "x", Status: StatusPlanned}); err == nil {
		t.Fatal("PutTask on closed db should error")
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
		{ID: "z-done-old", Status: StatusDone, DoneAt: ptr(t1)},
		{ID: "a-done-new", Status: StatusDone, DoneAt: ptr(t3)},
		{ID: "m-planned", Status: StatusPlanned, DoneAt: ptr(t2)},
		{ID: "active-2", Status: StatusWorking},
		{ID: "active-1", Status: StatusPlanning},
		{ID: "active-3", Status: StatusVerifying},
		{ID: "active-4", Status: StatusHelp},
	}
	SortTasks(tasks)

	wantIDs := []string{
		"active-1", "active-2", "active-3", "active-4",
		"a-done-new", "m-planned", "z-done-old",
	}
	for i, id := range wantIDs {
		if tasks[i].ID != id {
			t.Fatalf("tasks[%d].ID = %q, want %q (got order: %v)", i, tasks[i].ID, id, idsOf(tasks))
		}
	}
}

// TestSortTasks_FallbackTimes drives the work_end_at and plan_end_at
// branches in taskFallbackTime by leaving DoneAt nil.
func TestSortTasks_FallbackTimes(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	tasks := []Task{
		{ID: "plan-only", Status: StatusPlanned, PlanEndAt: ptr(t1)},
		{ID: "work-end", Status: StatusPlanned, WorkEndAt: ptr(t3)},
		{ID: "no-time", Status: StatusPlanned},
		{ID: "done", Status: StatusDone, DoneAt: ptr(t2)},
	}
	SortTasks(tasks)
	want := []string{"work-end", "done", "plan-only", "no-time"}
	if got := idsOf(tasks); !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestSortTasks_TieBreakers drives the equal-time path (ID descending
// for inactive) and the equal-active path (ID ascending).
func TestSortTasks_TieBreakers(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tasks := []Task{
		{ID: "inactive-a", Status: StatusDone, DoneAt: &at},
		{ID: "inactive-b", Status: StatusDone, DoneAt: &at},
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
