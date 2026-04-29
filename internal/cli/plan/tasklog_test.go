package plan

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

// readTasks opens the per-cwd tasks DB, lists every task, and closes
// the store. Tests call this after Run to assert the lifecycle wrote
// what we expect.
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

func TestRun_Markdown_LogsPlannedTask(t *testing.T) {
	t.Chdir(t.TempDir())
	target := writeTarget(t, "# spec heading\nbody text")
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		Target:      target,
		Interactive: true,
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1: %+v", len(tasks), tasks)
	}
	got := tasks[0]
	if got.Status != store.StatusPlanned {
		t.Fatalf("Status = %q, want planned", got.Status)
	}
	if got.InvokedTool != "cursor" || got.InvokedModel != "sonnet-4" {
		t.Fatalf("tool/model = %q/%q", got.InvokedTool, got.InvokedModel)
	}
	if got.RequirementMarkdown == "" {
		t.Fatal("requirement should be the file body")
	}
	if got.PlanMarkdown == nil || *got.PlanMarkdown == "" {
		t.Fatalf("PlanMarkdown should be set on success: %v", got.PlanMarkdown)
	}
	if got.Summary != "spec heading" {
		t.Fatalf("Summary = %q, want %q", got.Summary, "spec heading")
	}
	if got.PlanBeginAt == nil || got.PlanEndAt == nil {
		t.Fatalf("timestamps should be set: begin=%v end=%v", got.PlanBeginAt, got.PlanEndAt)
	}
	if got.PlanEndAt.Before(*got.PlanBeginAt) {
		t.Fatalf("end %v before begin %v", got.PlanEndAt, got.PlanBeginAt)
	}
	if got.ResumeCursor != filepath.Dir(target) {
		t.Fatalf("ResumeCursor = %q, want %q", got.ResumeCursor, filepath.Dir(target))
	}
}

func TestRun_Markdown_AgentError_LogsHelpStatus(t *testing.T) {
	t.Chdir(t.TempDir())
	target := writeTarget(t, "x")
	agent := newScriptedAgent()
	agent.planErr = errors.New("agent boom")
	err := Run(context.Background(), Options{
		Target: target,
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
		t.Fatalf("tasks = %+v, want one help-status task", tasks)
	}
	if tasks[0].PlanMarkdown != nil {
		t.Fatalf("PlanMarkdown should stay nil on failure: %v", tasks[0].PlanMarkdown)
	}
}

// TestRun_Markdown_AgentSkippedWrite_LeavesPlanMarkdownNil pins the
// "agent succeeded but did not write the output file" branch: the task
// is marked planned but PlanMarkdown stays nil because we cannot read
// the file we never produced.
func TestRun_Markdown_AgentSkippedWrite_LeavesPlanMarkdownNil(t *testing.T) {
	t.Chdir(t.TempDir())
	target := writeTarget(t, "x")
	agent := newScriptedAgent()
	agent.skipWrite = true
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: &stderr,
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
	if tasks[0].Status != store.StatusPlanned {
		t.Fatalf("Status = %q, want planned", tasks[0].Status)
	}
	if tasks[0].PlanMarkdown != nil {
		t.Fatalf("PlanMarkdown should be nil when output file missing: %v", tasks[0].PlanMarkdown)
	}
}

func TestRun_Scratch_LogsPlannedTaskWithFallbackSummary(t *testing.T) {
	t.Chdir(t.TempDir())
	agent := newScriptedAgent()
	agent.skipWrite = true
	ui := &scriptedUI{source: SourceScratch}
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %+v", tasks)
	}
	got := tasks[0]
	if got.Status != store.StatusPlanned {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.Summary != "from scratch" {
		t.Fatalf("Summary = %q, want fallback", got.Summary)
	}
	if got.RequirementMarkdown != "" {
		t.Fatalf("RequirementMarkdown should be empty for scratch: %q", got.RequirementMarkdown)
	}
	if got.PlanMarkdown != nil {
		t.Fatalf("PlanMarkdown should be nil for scratch: %v", got.PlanMarkdown)
	}
	if got.ResumeCursor == "" {
		t.Fatal("scratch ResumeCursor should fall back to cwd, not empty")
	}
}

func TestRun_Scratch_AgentError_LogsHelpStatus(t *testing.T) {
	t.Chdir(t.TempDir())
	agent := newScriptedAgent()
	agent.planErr = errors.New("scratch boom")
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{source: SourceScratch},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusHelp {
		t.Fatalf("tasks = %+v, want one help task", tasks)
	}
}

// TestPlanSummary_Fallbacks pins the secondary and tertiary branches
// in planSummary (basename and constant label) without going through
// the full Run flow.
func TestPlanSummary_Fallbacks(t *testing.T) {
	cases := []struct {
		req, target, want string
	}{
		{"# heading\nbody", "/tmp/spec.md", "heading"},
		{"", "/tmp/spec.md", "spec.md"},
		{"", "", "from scratch"},
	}
	for _, c := range cases {
		if got := planSummary(c.req, c.target); got != c.want {
			t.Fatalf("planSummary(%q,%q) = %q, want %q", c.req, c.target, got, c.want)
		}
	}
}

// TestPlanResumeCursor pins the workspace and cwd-fallback branches.
func TestPlanResumeCursor(t *testing.T) {
	if got := planResumeCursor("/foo/bar/spec.md"); got != "/foo/bar" {
		t.Fatalf("planResumeCursor(target) = %q", got)
	}
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if got := planResumeCursor(""); got != cwd {
		t.Fatalf("planResumeCursor(\"\") = %q, want %q", got, cwd)
	}
}

// TestFinishPlan_Idempotent pins the closed-flag short-circuit so a
// second finishPlan call is a silent no-op.
func TestFinishPlan_Idempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	lc := beginPlanTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", "", "")
	lc.finishPlan(nil, "")
	lc.finishPlan(errors.New("boom"), "should not change anything")
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusPlanned {
		t.Fatalf("second finish should be a no-op: %+v", tasks)
	}
}

// TestBeginPlanTask_PutErrorWarns drives the "tasks put" warning path
// by closing the bbolt DB out from under the lifecycle (the underlying
// store helper succeeds at open, but PutTask then fails). We only
// exercise that warning is printed; the lifecycle still completes.
func TestBeginPlanTask_PutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	// Pre-create the DB and corrupt it by replacing the file with a
	// directory after open: too racy. Instead, drive the put error
	// path by hand: open a tasks store, close it, then call
	// PutTask via lifecycle by rewiring its store to that closed
	// instance.
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
	lc := &planLifecycle{stderr: &stderr, store: s, task: store.Task{
		ID:     store.NewTaskID(),
		Status: store.StatusPlanning,
	}}
	lc.finishPlan(nil, "")
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestBeginPlanTask_OpenTaskLogFails forces store.OpenTaskLog to
// return ok=false by making the tasks path a directory (so bolt.Open
// errors). The lifecycle must still produce a non-nil pointer with a
// nil store, and finishPlan on it must be a silent no-op (no panic).
func TestBeginPlanTask_OpenTaskLogFails(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultTasksPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	lc := beginPlanTask(Options{Stderr: &stderr}, &scriptedAgent{name: "cursor"}, "m", "", "")
	if lc.store != nil {
		t.Fatal("store should be nil after open failure")
	}
	lc.finishPlan(nil, "ignored")
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want some tasks warning", stderr.String())
	}
}

