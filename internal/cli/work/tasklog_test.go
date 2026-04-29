package work

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// readTasks lists every task in the per-cwd tasks DB. Tests call this
// after Run to assert the lifecycle wrote what we expect.
func readTasks(t *testing.T) []store.Task {
	t.Helper()
	path, err := store.DefaultTasksPath()
	if err != nil {
		t.Fatalf("DefaultTasksPath: %v", err)
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

func TestRun_LogsDoneTask_WithSidecar(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("plan body content"), 0o600); err != nil {
		t.Fatal(err)
	}
	requirement := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(requirement, []byte("# original heading\noriginal body"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d", len(tasks))
	}
	got := tasks[0]
	if got.Status != store.StatusDone {
		t.Fatalf("Status = %q, want done", got.Status)
	}
	if got.RequirementMarkdown == "" || !strings.Contains(got.RequirementMarkdown, "original heading") {
		t.Fatalf("RequirementMarkdown = %q", got.RequirementMarkdown)
	}
	if got.PlanMarkdown == nil || *got.PlanMarkdown != "plan body content" {
		t.Fatalf("PlanMarkdown = %v", got.PlanMarkdown)
	}
	if got.Summary != "original heading" {
		t.Fatalf("Summary = %q, want first heading", got.Summary)
	}
	if got.WorkBeginAt == nil || got.WorkEndAt == nil || got.DoneAt == nil {
		t.Fatalf("timestamps incomplete: %+v", got)
	}
	if got.ResumeCursor != filepath.Dir(plan) {
		t.Fatalf("ResumeCursor = %q", got.ResumeCursor)
	}
}

func TestRun_NoSidecar_LogsTaskWithEmptyRequirement(t *testing.T) {
	t.Chdir(t.TempDir())
	plan := writePlan(t, "plan only")
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d", len(tasks))
	}
	if tasks[0].RequirementMarkdown != "" {
		t.Fatalf("RequirementMarkdown should be empty when sidecar missing: %q", tasks[0].RequirementMarkdown)
	}
	if tasks[0].Summary != "plan only" {
		t.Fatalf("Summary = %q, want plan-body fallback", tasks[0].Summary)
	}
}

func TestRun_AgentError_LogsHelpStatus(t *testing.T) {
	t.Chdir(t.TempDir())
	plan := writePlan(t, "x")
	agent := newScriptedAgent()
	agent.workErr = errors.New("agent boom")
	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusHelp {
		t.Fatalf("tasks = %+v, want one help task", tasks)
	}
	if tasks[0].DoneAt != nil {
		t.Fatalf("DoneAt should be nil on failure: %v", tasks[0].DoneAt)
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
		{"", "", "", "work session"},
	}
	for _, c := range cases {
		if got := workSummary(c.req, c.plan, c.planPath); got != c.want {
			t.Fatalf("workSummary(%q,%q,%q) = %q, want %q", c.req, c.plan, c.planPath, got, c.want)
		}
	}
}

func TestFinishWork_Idempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	plan := writePlan(t, "body")
	lc := beginWorkTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", plan, "body")
	lc.finishWork(nil)
	lc.finishWork(errors.New("ignored"))
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusDone {
		t.Fatalf("second finish should be a no-op: %+v", tasks)
	}
}

// TestBeginWorkTask_OpenTaskLogFails forces store.OpenTaskLog to
// return ok=false by making the tasks path a directory; finishWork on
// the resulting nil-store lifecycle is a silent no-op.
func TestBeginWorkTask_OpenTaskLogFails(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultTasksPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	lc := beginWorkTask(Options{Stderr: &stderr}, &scriptedAgent{name: "cursor"}, "m", "/tmp/x.plan.md", "body")
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
	path, err := store.DefaultTasksPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
