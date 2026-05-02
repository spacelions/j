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

	bolt "go.etcd.io/bbolt"

	"github.com/spacelions/j/internal/store"
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
func openTasksDB(t *testing.T) *store.Store {
	t.Helper()
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
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

// TestNew_HasDeleteSubcommand pins the registration of the delete
// child so the parent's constructor always exposes it. Detailed
// flag/runtime behavior of the child lives in delete_test.go.
func TestNew_HasDeleteSubcommand(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == "delete" {
			return
		}
	}
	t.Fatal("expected `delete` subcommand to be registered on `j tasks`")
}

// TestRun_NoTasksFile_PrintsEmptyMessage covers the defense-in-depth
// short-circuit in listTasks: when list.db is missing it returns
// emptyMessage instead of a stat error. We bypass the cobra layer
// (and its pre-flight) so the missing-file state survives long enough
// to reach the branch.
func TestRun_NoTasksFile_PrintsEmptyMessage(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	if err := listTasks(&out); err != nil {
		t.Fatalf("listTasks: %v", err)
	}
	if !strings.Contains(out.String(), emptyMessage) {
		t.Fatalf("stdout = %q, want %q", out.String(), emptyMessage)
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

// TestRun_PrintsHeaderAndSortedTasks pins the table layout: header
// first, summary rows in active-then-by-phase-end order. The three
// per-phase session lines that earlier versions emitted are gone, so
// the output is exactly header + 1 line per task. Active tasks should
// sort before inactive ones; among inactive tasks the most recent
// phase-end wins.
func TestRun_PrintsHeaderAndSortedTasks(t *testing.T) {
	s := openTasksDB(t)
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)

	tasks := []store.Task{
		{
			ID:               "ddd-old-plan-done",
			Status:           store.StatusPlanDone,
			InvokedTool:      "cursor",
			InvokedModel:     "gpt-5",
			PlanResumeCursor: "",
			Summary:          "old one",
			PlanEndAt:        &t1,
		},
		{
			ID:               "aaa-new-work-done",
			Status:           store.StatusWorkDone,
			InvokedTool:      "cursor",
			InvokedModel:     "sonnet-4",
			PlanResumeCursor: "8c7e6a9d-0f1a-4b2c-9d8e-1234567890ab",
			WorkResumeCursor: "11111111-2222-3333-4444-555555555555",
			Summary:          "new one",
			WorkEndAt:        &t2,
		},
		{
			ID:               "active-1",
			Status:           store.StatusPlanning,
			InvokedTool:      "cursor",
			InvokedModel:     "sonnet-4",
			PlanResumeCursor: "11111111-1111-4111-9111-111111111111",
			Summary:          "draft idea",
		},
	}
	for _, task := range tasks {
		if err := s.PutTask(task); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	out, _, err := runCommand(t)
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

// TestRun_HidesSessionLines pins the contract that `j tasks` no longer
// emits the indented `plan session:` / `work session:` /
// `verify session:` lines, even when the task has non-empty resume
// cursors for every phase.
func TestRun_HidesSessionLines(t *testing.T) {
	s := openTasksDB(t)
	task := store.Task{
		ID:                 "all-cursors",
		Status:             store.StatusPlanDone,
		InvokedTool:        "cursor",
		InvokedModel:       "sonnet-4",
		PlanResumeCursor:   "plan-cursor-id",
		WorkResumeCursor:   "work-cursor-id",
		VerifyResumeCursor: "verify-cursor-id",
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

// TestRun_StatNonENOENTPropagates makes the list.db path a directory
// holding a file so os.Stat succeeds (it's a directory) but bolt.Open
// fails when listTasks tries to open it. This exercises the non-
// ENOENT propagation path for the underlying open error. We bypass
// cobra so pre-flight does not heal the corrupt layout.
func TestRun_StatNonENOENTPropagates(t *testing.T) {
	t.Chdir(t.TempDir())
	tasksPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tasksPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksPath, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := listTasks(io.Discard); err == nil {
		t.Fatal("expected open to fail when path is a non-empty directory")
	}
}

// TestRun_DefaultTasksPathError replaces the cwd with one we then
// remove so DefaultTasksDBPath -> os.Getwd fails. On macOS getwd may
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
	if err := listTasks(io.Discard); err == nil {
		t.Fatal("expected DefaultTasksDBPath to surface getwd error")
	}
}

// TestRun_OpenError points the tasks DB path at an existing directory
// so bolt.Open fails, exercising the open-error branch in listTasks.
// We bypass cobra (and pre-flight) so the corrupt layout survives.
func TestRun_OpenError(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := listTasks(io.Discard); err == nil {
		t.Fatal("expected open error when tasks path is a directory")
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
func writeRawTaskBytes(t *testing.T, key string, value []byte) error {
	t.Helper()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return err
	}
	defer db.Close()
	return db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(store.BucketTasks))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), value)
	})
}

// TestWriteTasks_FlushError exercises the tabwriter flush error path
// by passing a writer that fails on every Write.
func TestWriteTasks_FlushError(t *testing.T) {
	err := writeTasks(failingWriter{}, []store.Task{
		{ID: "x", Status: store.StatusPlanDone},
	})
	if err == nil {
		t.Fatal("expected error from failing writer")
	}
}

// TestFormatSession pins the rendering of the indented session line:
// empty ids become "-", non-empty ids are echoed verbatim, and the
// label/indent are constant prefixes the tests can rely on.
func TestFormatSession(t *testing.T) {
	if got, want := formatSession("plan session", ""), "  plan session: -"; got != want {
		t.Fatalf("empty: got %q, want %q", got, want)
	}
	if got, want := formatSession("work session", "uuid-1"), "  work session: uuid-1"; got != want {
		t.Fatalf("uuid: got %q, want %q", got, want)
	}
	if got, want := formatSession("verify session", "abc"), "  verify session: abc"; got != want {
		t.Fatalf("verify: got %q, want %q", got, want)
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
