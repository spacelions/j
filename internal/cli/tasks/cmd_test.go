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

// openTasksDB chdirs to a fresh temp dir and opens the tasks DB. The
// caller is responsible for closing the store before running `j tasks`
// because bbolt holds an exclusive file lock and the command opens its
// own store from the same path.
func openTasksDB(t *testing.T) *store.Store {
	t.Helper()
	t.Chdir(t.TempDir())
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir tasks dir: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
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

func TestRun_NoTasksFile_PrintsEmptyMessage(t *testing.T) {
	t.Chdir(t.TempDir())
	out, _, err := runCommand(t)
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

// TestRun_PrintsHeaderAndSortedTasks pins the table layout: header
// first, summary rows in active-then-by-phase-end order, and three
// indented session lines per task. Active tasks should sort before
// inactive ones; among inactive tasks the most recent phase-end wins.
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
	// Header + 3 tasks * (1 summary + 3 session lines) = 1 + 12 = 13.
	if len(lines) < 13 {
		t.Fatalf("output has fewer rows than expected: %q", out)
	}
	if !strings.HasPrefix(lines[0], "ID") || !strings.Contains(lines[0], "STATUS") {
		t.Fatalf("missing header: %q", lines[0])
	}
	// The new layout drops the RESUME column from the table; resume
	// ids only appear on the indented session lines below each row.
	if strings.Contains(lines[0], "RESUME") {
		t.Fatalf("header should not contain RESUME column: %q", lines[0])
	}
	if !strings.Contains(out, "8c7e6a9d-0f1a-4b2c-9d8e-1234567890ab") || !strings.Contains(out, "11111111-1111-4111-9111-111111111111") {
		t.Fatalf("expected resume session ids in output: %q", out)
	}
	if !strings.Contains(out, "11111111-2222-3333-4444-555555555555") {
		t.Fatalf("expected work session id in output: %q", out)
	}
	// Active first, then most-recent phase-end-at first among inactive.
	wantOrder := []string{"active-1", "aaa-new-work-done", "ddd-old-plan-done"}
	summaryRows := []string{lines[1], lines[5], lines[9]}
	for i, id := range wantOrder {
		if !strings.Contains(summaryRows[i], id) {
			t.Fatalf("summary row %d = %q, want substring %q", i, summaryRows[i], id)
		}
	}
	// Each summary row is followed by exactly three indented session
	// lines in plan/work/verify order.
	if !strings.Contains(lines[2], "plan session:") {
		t.Fatalf("expected plan session line: %q", lines[2])
	}
	if !strings.Contains(lines[3], "work session:") {
		t.Fatalf("expected work session line: %q", lines[3])
	}
	if !strings.Contains(lines[4], "verify session:") {
		t.Fatalf("expected verify session line: %q", lines[4])
	}
	// Empty cursors should render as a dash on the active task.
	if !strings.Contains(lines[4], "verify session: -") {
		t.Fatalf("empty verify cursor should show '-': %q", lines[4])
	}
	if !strings.Contains(out, "planning") || !strings.Contains(out, "plan-done") || !strings.Contains(out, "work-done") {
		t.Fatalf("status column missing: %q", out)
	}
}

// TestRun_StatNonENOENTPropagates makes the index.db path a directory
// holding a file so os.Stat succeeds (it's a directory) but bolt.Open
// fails when listTasks tries to open it. This exercises the non-
// ENOENT propagation path for the underlying open error.
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
	_, _, err = runCommand(t)
	if err == nil {
		t.Fatal("expected open to fail when path is a non-empty directory")
	}
}

// TestRun_DefaultTasksPathError replaces the cwd with one we then
// remove so DefaultTasksDBPath -> os.Getwd fails. On macOS getwd may
// still succeed via cached inodes; in that case the test skips.
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
	_, _, err := runCommand(t)
	if err == nil {
		t.Fatal("expected DefaultTasksDBPath to surface getwd error")
	}
}

// TestRun_OpenError points the tasks DB path at an existing directory
// so bolt.Open fails, exercising the open-error branch.
func TestRun_OpenError(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCommand(t); err == nil {
		t.Fatal("expected open error when tasks path is a directory")
	}
}

// TestRun_DecodeError plants a non-JSON value into the tasks bucket so
// ListTasks returns a decode error and runList propagates it.
func TestRun_DecodeError(t *testing.T) {
	t.Chdir(t.TempDir())
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
