package tasks

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestMain chdirs the test binary into a temp dir so each test starts
// with an "empty .j/tasks" world unless it explicitly seeds one.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "tasks-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	if err := os.Chdir(tmp); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func runCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// openTasksDB chdirs to a fresh temp dir, runs mustInit (the new
// pre-flight contract), and opens the tasks DB. The caller is
// responsible for closing the store before running `j tasks` because
// bbolt holds an exclusive file lock and the command opens its own
// store from the same path.
func openTasksDB(t *testing.T) *tasks.Store {
	t.Helper()
	t.Chdir(t.TempDir())
	mustInit(t)
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	return s
}

// mustInit lays down the .j layout in the current working directory.
// Tests must call this helper after t.Chdir so the new pre-flight
// contract is satisfied (otherwise the j tasks command intercepts
// with the init prompt). Idempotent.
func mustInit(t *testing.T) {
	t.Helper()
	testutil.Init(t)
}

func TestNew_Smoke(t *testing.T) {
	cmd := New()
	if cmd == nil {
		t.Fatal("New returned nil")
	}
	if cmd.Use != "tasks" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Fatal("RunE is nil")
	}
}

// TestNew_HasDiscardSubcommand pins the registration of the discard
// child so the parent's constructor always exposes it. Detailed
// flag/runtime behavior of the child lives in discard_test.go.
func TestNew_HasDiscardSubcommand(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == "discard" {
			return
		}
	}
	t.Fatal("expected `discard` subcommand to be registered on `j tasks`")
}

// TestNew_HasReVerifySubcommand pins the registration of the
// re-verify child.
func TestNew_HasReVerifySubcommand(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == "re-verify" {
			return
		}
	}
	t.Fatal("expected `re-verify` subcommand to be registered on `j tasks`")
}

// TestNew_HasResumeVerifySubcommand pins the registration of the
// resume-verify child.
func TestNew_HasResumeVerifySubcommand(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == "resume-verify" {
			return
		}
	}
	t.Fatal("expected `resume-verify` subcommand to be registered on `j tasks`")
}

// TestRun_NoTasksFile_PrintsEmptyMessage covers the defense-in-depth
// short-circuit in listTasks: when list.db is missing it returns
// emptyMessage instead of a stat error. We bypass the cobra layer
// (and its pre-flight) so the missing-file state survives long enough
// to reach the branch.
func TestRun_NoTasksFile_PrintsEmptyMessage(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	if err := listTasks(&out, false); err != nil {
		t.Fatalf("listTasks: %v", err)
	}
	if !strings.Contains(out.String(), emptyMessage) {
		t.Fatalf("stdout = %q, want %q", out.String(), emptyMessage)
	}
}

// TestRun_EmptyDB_Simple confirms the empty-DB short-circuit fires
// for the --simple path too: dispatch happens after the empty check,
// so all three renderers see the same emptyMessage line.
func TestRun_EmptyDB_Simple(t *testing.T) {
	s := openTasksDB(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCommand(t, "--simple")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, emptyMessage) {
		t.Fatalf("stdout = %q, want %q", out, emptyMessage)
	}
}

func TestRun_EmptyDB_PrintsEmptyMessage(t *testing.T) {
	s := openTasksDB(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCommand(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, emptyMessage) {
		t.Fatalf("stdout = %q, want %q", out, emptyMessage)
	}
}

// TestRun_PrintsHeaderAndSortedTasks pins the --simple table layout:
// header first, summary rows in active-then-by-phase-end order. The
// three per-phase session lines that earlier versions emitted are
// gone, so the output is exactly header + 1 line per task. Active
// tasks should sort before inactive ones; among inactive tasks the
// most recent phase-end wins. The default (bordered) renderer adds
// frame lines, so this assertion lives behind --simple where the
// tabwriter output is preserved verbatim.
func TestRun_PrintsHeaderAndSortedTasks(t *testing.T) {
	s := openTasksDB(t)
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)

	rows := []tasks.Task{
		{
			ID:               "ddd-old-plan-done",
			Status:           tasks.StatusPlanDone,
			PlanTool:         "cursor",
			PlanModel:        "gpt-5",
			PlanResumeSession: "",
			Summary:          "old one",
			PlanEndAt:        t1,
		},
		{
			ID:               "aaa-new-work-done",
			Status:           tasks.StatusWorkDone,
			WorkTool:         "cursor",
			WorkModel:        "sonnet-4",
			PlanResumeSession: "8c7e6a9d-0f1a-4b2c-9d8e-1234567890ab",
			WorkResumeSession: "11111111-2222-3333-4444-555555555555",
			Summary:          "new one",
			WorkEndAt:        t2,
		},
		{
			ID:               "active-1",
			Status:           tasks.StatusPlanning,
			PlanTool:         "cursor",
			PlanModel:        "sonnet-4",
			PlanResumeSession: "11111111-1111-4111-9111-111111111111",
			Summary:          "draft idea",
		},
	}
	for _, row := range rows {
		if err := s.PutTask(row); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	out, _, err := runCommand(t, "--simple")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lines := splitLines(out)
	// Header + 3 task summary rows = 4 lines. Session lines are gone.
	if len(lines) != 4 {
		t.Fatalf("output should be header + 3 summary rows, got %d lines: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "ID") || !strings.Contains(lines[0], "STATUS") {
		t.Fatalf("missing header: %q", lines[0])
	}
	// The new layout drops the RESUME column from the table; resume
	// ids should not surface on this listing at all.
	if strings.Contains(lines[0], "RESUME") {
		t.Fatalf("header should not contain RESUME column: %q", lines[0])
	}
	for _, banned := range []string{
		"8c7e6a9d-0f1a-4b2c-9d8e-1234567890ab",
		"11111111-1111-4111-9111-111111111111",
		"11111111-2222-3333-4444-555555555555",
	} {
		if strings.Contains(out, banned) {
			t.Fatalf("session id %q should not surface in `j tasks` output: %q", banned, out)
		}
	}
	for _, banned := range []string{"plan session:", "work session:", "verify session:"} {
		if strings.Contains(out, banned) {
			t.Fatalf("`%s` line should be hidden: %q", banned, out)
		}
	}
	// Active first, then most-recent phase-end-at first among inactive.
	wantOrder := []string{"active-1", "aaa-new-work-done", "ddd-old-plan-done"}
	summaryRows := []string{lines[1], lines[2], lines[3]}
	for i, id := range wantOrder {
		if !strings.Contains(summaryRows[i], id) {
			t.Fatalf("summary row %d = %q, want substring %q", i, summaryRows[i], id)
		}
	}
	if !strings.Contains(out, "planning") || !strings.Contains(out, "plan-done") || !strings.Contains(out, "work-done") {
		t.Fatalf("status column missing: %q", out)
	}
}

// TestRun_DefaultNonTTY_RendersBorder drives the default (no
// --simple) renderer when stdout is a *bytes.Buffer. isTerminal
// rejects anything that isn't an *os.File so the dispatch falls
// through to uitheme.WriteTaskTable rather than the bubbletea TUI; the
// resulting output should carry the lipgloss border glyphs and
// decorate the active row's status with elapsed time.
func TestRun_DefaultNonTTY_RendersBorder(t *testing.T) {
	s := openTasksDB(t)
	begin := time.Now().UTC().Add(-90 * time.Second)
	task := tasks.Task{
		ID:       "active-default",
		Status:   tasks.StatusPlanning,
		PlanTool: "cursor",
		PlanModel: "sonnet-4",
		Summary:      "draft idea",
		PlanBeginAt:  begin,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	out, _, err := runCommand(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, glyph := range []string{"╭", "╮", "╰", "╯", "│", "─"} {
		if !strings.Contains(out, glyph) {
			t.Fatalf("default output missing border glyph %q: %q", glyph, out)
		}
	}
	if !strings.Contains(out, "STATUS") {
		t.Fatalf("default output missing header: %q", out)
	}
	if !strings.Contains(out, "planning(1m") {
		t.Fatalf("default output should decorate active row with elapsed minutes: %q", out)
	}
}

// TestRun_HidesSessionLines pins the contract that `j tasks` no longer
// emits the indented `plan session:` / `work session:` /
// `verify session:` lines, even when the task has non-empty resume
// cursors for every phase.
func TestRun_HidesSessionLines(t *testing.T) {
	s := openTasksDB(t)
	task := tasks.Task{
		ID:       "all-cursors",
		Status:   tasks.StatusPlanDone,
		PlanTool: "cursor",
		PlanModel: "sonnet-4",
		PlanResumeSession:   "plan-cursor-id",
		WorkResumeSession:   "work-cursor-id",
		VerifyResumeSession: "verify-cursor-id",
		Summary:            "all cursors set",
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	out, _, err := runCommand(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, banned := range []string{
		"plan session:",
		"work session:",
		"verify session:",
		"plan-cursor-id",
		"work-cursor-id",
		"verify-cursor-id",
	} {
		if strings.Contains(out, banned) {
			t.Fatalf("output unexpectedly contains %q: %q", banned, out)
		}
	}
}

// TestRun_DefaultTasksPathError replaces the cwd with one we then
// remove so DefaultTasksDir -> os.Getwd fails. On macOS getwd may
// still succeed via cached inodes; in that case the test skips. We
// bypass cobra (and pre-flight) so the broken cwd reaches listTasks.
func TestRun_DefaultTasksPathError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may bypass relevant FS errors")
	}
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	t.Setenv("PWD", "")
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("os.Getwd unexpectedly succeeded; cannot exercise failure path")
	}
	if err := listTasks(io.Discard, false); err == nil {
		t.Fatal("expected DefaultTasksDir to surface getwd error")
	}
}

// TestRun_DecodeError plants a non-JSON value into the tasks bucket so
// ListTasks returns a decode error and runList propagates it. The
// seeded list.db satisfies pre-flight so the cobra path runs end-to-
// end and surfaces the decode error rather than the init prompt.
func TestRun_DecodeError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if err := writeRawTaskBytes(t, "bad", []byte("not-json")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, _, err := runCommand(t)
	if err == nil || !strings.Contains(err.Error(), `decode task "bad"`) {
		t.Fatalf("err = %v", err)
	}
}

// writeRawTaskBytes opens the tasks DB at the test's cwd and writes a
// raw value under the tasks bucket. It's a low-level helper used to
// drive the JSON decode failure branch in ListTasks.
// writeRawTaskBytes plants a raw byte payload as `<id>/task.toml`
// under the per-cwd tasks dir. Used by decode-error tests that need
// to seed a malformed row without going through PutTask's encoder.
func writeRawTaskBytes(t *testing.T, id string, value []byte) error {
	t.Helper()
	dir, err := tasks.DefaultDir()
	if err != nil {
		return err
	}
	taskDir := filepath.Join(dir, id)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(taskDir, tasks.TaskFileName), value, 0o644)
}

// TestStoreReloader_SortsAndReaps drives the closure handed to the
// bubbletea model. It seeds two rows in non-sorted order and confirms
// the reloader returns them active-first / inactive-by-end-time so
// each tick re-renders an up-to-date table.
func TestStoreReloader_SortsAndReaps(t *testing.T) {
	s := openTasksDB(t)
	t.Cleanup(func() { _ = s.Close() })
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.PutTask(tasks.Task{
		ID: "z-old", Status: tasks.StatusPlanDone, PlanEndAt: t1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTask(tasks.Task{
		ID: "a-active", Status: tasks.StatusPlanning,
	}); err != nil {
		t.Fatal(err)
	}
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := storeReloader(s, tasksDir)()
	if err != nil {
		t.Fatalf("reloader: %v", err)
	}
	if len(got) != 2 || got[0].ID != "a-active" || got[1].ID != "z-old" {
		t.Fatalf("reloader returned unsorted slice: %#v", got)
	}
}

// TestStoreReloader_PropagatesListErr seeds a non-JSON value in the
// tasks bucket so ListTasks returns a decode error; the reloader must
// surface it without swallowing.
func TestStoreReloader_PropagatesListErr(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if err := writeRawTaskBytes(t, "bad", []byte("not-json")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := storeReloader(s, s.Dir())(); err == nil {
		t.Fatal("expected decode error to propagate from reloader")
	}
}

// TestIsTerminal_NonFileWriter pins the writer-type guard: anything
// that isn't an *os.File is treated as a non-TTY so tests reliably
// take the one-shot bordered path.
func TestIsTerminal_NonFileWriter(t *testing.T) {
	if isTerminal(io.Discard) {
		t.Fatal("io.Discard must not be classified as a TTY")
	}
	if isTerminal(&bytes.Buffer{}) {
		t.Fatal("*bytes.Buffer must not be classified as a TTY")
	}
}

// TestIsTerminal_PipeFile checks the *os.File branch: an os.Pipe
// reader/writer is a real *os.File but not an interactive TTY, so
// term.IsTerminal returns false.
func TestIsTerminal_PipeFile(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	if isTerminal(w) {
		t.Fatal("os.Pipe writer must not be classified as a TTY")
	}
}

// TestTerminalWidth covers both branches: a non-File writer always
// reports 0; an *os.File that isn't a TTY surfaces the term.GetSize
// error path which also reports 0.
func TestTerminalWidth(t *testing.T) {
	if got := terminalWidth(io.Discard); got != 0 {
		t.Fatalf("io.Discard width = %d, want 0", got)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	if got := terminalWidth(w); got != 0 {
		t.Fatalf("os.Pipe writer width = %d, want 0 (GetSize fails on non-TTY)", got)
	}
}

// TestWriteTasks_FlushError exercises the tabwriter flush error path
// by passing a writer that fails on every Write.
func TestWriteTasks_FlushError(t *testing.T) {
	err := writeTasks(failingWriter{}, []tasks.Task{
		{ID: "x", Status: tasks.StatusPlanDone},
	})
	if err == nil {
		t.Fatal("expected error from failing writer")
	}
}

// TestWriteTasks_EmptyHeaderFlushError covers the no-tasks branch:
// even with no rows the header is written and Flush surfaces the
// failingWriter error.
func TestWriteTasks_EmptyHeaderFlushError(t *testing.T) {
	err := writeTasks(failingWriter{}, nil)
	if err == nil {
		t.Fatal("expected flush error from failingWriter even with no rows")
	}
}

// failingWriter returns an error on every Write so writeTasks's
// tabwriter Flush fails.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, io.ErrShortWrite
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
