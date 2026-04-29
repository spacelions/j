package work

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// readTasks lists every task in the per-cwd tasks DB. Tests call this
// after Run to assert the lifecycle wrote what we expect.
func readTasks(t *testing.T) []store.Task {
	t.Helper()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	s, err := store.Open(path)
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

// TestBeginWorkTaskNew_RecordsRow pins the legacy import bbolt write:
// a fresh task row is created with status=working, the requested
// work-phase fields populated, and no plan-phase fields populated
// (since the legacy importer never had a plan phase).
func TestBeginWorkTaskNew_RecordsRow(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	taskID := store.NewTaskID()
	lc := beginWorkTaskNew(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", taskID, "/tmp/spec.plan.md", "# req", "plan body", "work-cursor")
	lc.finishWork(nil)
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].ID != taskID {
		t.Fatalf("tasks = %+v", tasks)
	}
	got := tasks[0]
	if got.Status != store.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
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

// TestBeginWorkTaskReuse_PreservesPlanPhase pins the bbolt-sourced
// reuse path: the existing plan-phase fields stay intact, only the
// work-phase fields and tool/model/resume-cursor are overwritten.
func TestBeginWorkTaskReuse_PreservesPlanPhase(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "seeded", "plan body", "req body")
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
	prePlanBegin := existing.PlanBeginAt
	prePlanEnd := existing.PlanEndAt
	preCursor := existing.PlanResumeCursor

	lc := beginWorkTaskReuse(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "gpt-5", existing, "fresh-work-cursor")
	lc.finishWork(nil)

	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].ID != id {
		t.Fatalf("expected one row: %+v", tasks)
	}
	got := tasks[0]
	if got.Status != store.StatusWorkDone {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.PlanResumeCursor != preCursor {
		t.Fatalf("PlanResumeCursor changed: got %q, want %q", got.PlanResumeCursor, preCursor)
	}
	if got.WorkResumeCursor != "fresh-work-cursor" {
		t.Fatalf("WorkResumeCursor = %q", got.WorkResumeCursor)
	}
	if got.InvokedModel != "gpt-5" {
		t.Fatalf("InvokedModel = %q, want gpt-5", got.InvokedModel)
	}
	if got.PlanBeginAt == nil || !got.PlanBeginAt.Equal(*prePlanBegin) {
		t.Fatalf("PlanBeginAt = %v, want %v", got.PlanBeginAt, prePlanBegin)
	}
	if got.PlanEndAt == nil || !got.PlanEndAt.Equal(*prePlanEnd) {
		t.Fatalf("PlanEndAt = %v, want %v", got.PlanEndAt, prePlanEnd)
	}
	if got.WorkBeginAt == nil || got.WorkEndAt == nil {
		t.Fatalf("work timestamps missing: %+v", got)
	}
}

// TestFinishWork_ErrorPath drives the StatusHelp branch when the
// underlying agent.Work errored.
func TestFinishWork_ErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	lc := beginWorkTaskNew(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "m", store.NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.finishWork(errors.New("boom"))
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusHelp {
		t.Fatalf("tasks = %+v, want one help task", tasks)
	}
	if tasks[0].DoneAt != nil {
		t.Fatalf("DoneAt should remain nil on failure: %v", tasks[0].DoneAt)
	}
}

func TestReadRequirementSidecar_Variants(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readRequirementSidecar(plan); got != "" {
		t.Fatalf("missing sidecar = %q, want empty", got)
	}
	requirement := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(requirement, []byte("req"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readRequirementSidecar(plan); got != "req" {
		t.Fatalf("present sidecar = %q, want req", got)
	}
	if got := readRequirementSidecar(""); got != "" {
		t.Fatalf("empty path = %q", got)
	}
	// A bare ".plan.md" path: stem is empty after stripping; the
	// helper must return "" instead of resolving to "<dir>/.md".
	if got := readRequirementSidecar(filepath.Join(dir, ".plan.md")); got != "" {
		t.Fatalf("empty stem = %q", got)
	}
}

// TestReadRequirementSidecar_CandidateEqualsPlan covers the
// "candidate == planPath" guard: when a non-conventional plan name
// would resolve to itself as the requirement we must not loop on
// reading the same file. We use a plan name that does NOT end in
// `.plan.md` so the trim leaves the stem alone and the candidate
// becomes identical to the input.
func TestReadRequirementSidecar_CandidateEqualsPlan(t *testing.T) {
	dir := t.TempDir()
	bare := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(bare, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readRequirementSidecar(bare); got != "" {
		t.Fatalf("self-sidecar = %q, want empty", got)
	}
}

func TestWorkSummary_Fallbacks(t *testing.T) {
	cases := []struct {
		req, plan, planPath, want string
	}{
		{"# req heading\nbody", "## plan", "/tmp/x.plan.md", "req heading"},
		{"", "## plan heading", "/tmp/x.plan.md", "plan heading"},
		{"", "", "/tmp/x.plan.md", "x.plan.md"},
		{"", "", "", ""},
	}
	for _, c := range cases {
		if got := workSummary(c.req, c.plan, c.planPath); got != c.want {
			t.Fatalf("workSummary(%q,%q,%q) = %q, want %q", c.req, c.plan, c.planPath, got, c.want)
		}
	}
}

func TestFinishWork_Idempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	lc := beginWorkTaskNew(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", store.NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.finishWork(nil)
	lc.finishWork(errors.New("ignored"))
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusWorkDone {
		t.Fatalf("second finish should be a no-op: %+v", tasks)
	}
}

// TestOpenLifecycle_OpenTaskLogFails forces openTaskLog to return
// ok=false by replacing the post-init list.db file with a directory;
// finishWork on the resulting nil-store lifecycle is a silent no-op.
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
	lc := beginWorkTaskNew(Options{Stderr: &stderr}, &scriptedAgent{name: "cursor"}, "m", store.NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	if lc.store != nil {
		t.Fatal("store should be nil after open failure")
	}
	lc.finishWork(nil)
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want tasks warning", stderr.String())
	}
}

// TestFinishWork_PutErrorWarns drives the finalize-time put warning
// by injecting a closed store into the lifecycle so PutTask fails.
func TestFinishWork_PutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	lc := &workLifecycle{stderr: &stderr, store: s, task: store.Task{
		ID:     store.NewTaskID(),
		Status: store.StatusWorking,
	}}
	lc.finishWork(nil)
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
