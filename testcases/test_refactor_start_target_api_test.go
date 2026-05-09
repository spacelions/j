package testcases_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/resolver"
	storetasks "github.com/spacelions/j/internal/store/tasks"
)

// TestRefactor_StartTargetAPISurvives exercises the full
// start-target staging API (black-box) to confirm the refactor
// preserved every entry point and they compose correctly.
//
// Covers:
//   - NewStartTargetFromMarkdown reads a file and mint a StartTarget.
//   - NewStartTargetFromBody mints an in-memory StartTarget.
//   - PrepareStartTaskFiles creates a task dir, writes
//     requirements.md, and returns the agent-log path.
//   - StartTarget struct fields are correctly populated.
func TestRefactor_StartTargetAPISurvives(t *testing.T) {
	freshInit(t)
	// 1. From-file path: NewStartTargetFromMarkdown
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := resolver.NewStartTargetFromMarkdown(path)
	if err != nil {
		t.Fatalf("NewStartTargetFromMarkdown: %v", err)
	}
	if st.TaskID == "" {
		t.Fatal("TaskID is empty")
	}
	if !st.IsNew {
		t.Fatal("IsNew should be true for markdown source")
	}
	if st.Body != "hello world" {
		t.Fatalf("Body = %q, want %q", st.Body, "hello world")
	}
	if st.Source != path {
		t.Fatalf("Source = %q, want %q", st.Source, path)
	}

	// PrepareStartTaskFiles writes requirements.md underneath
	// the task dir.
	_, err = resolver.PrepareStartTaskFiles(st)
	if err != nil {
		t.Fatalf("PrepareStartTaskFiles: %v", err)
	}

	// 2. In-memory path: NewStartTargetFromBody
	st2 := resolver.NewStartTargetFromBody(
		"body text", "linear:ENG-42", "ENG-42",
	)
	if st2.TaskID == "" {
		t.Fatal("TaskID is empty")
	}
	if !st2.IsNew {
		t.Fatal("IsNew should be true for body source")
	}
	if st2.Body != "body text" {
		t.Fatalf("Body = %q, want %q", st2.Body, "body text")
	}
	if st2.Source != "linear:ENG-42" {
		t.Fatalf("Source = %q, want %q",
			st2.Source, "linear:ENG-42")
	}
	if st2.LinearIssue != "ENG-42" {
		t.Fatalf("LinearIssue = %q, want %q",
			st2.LinearIssue, "ENG-42")
	}

	// PrepareStartTaskFiles for body source creates the task dir
	// and stages requirements.md.
	logPath, err := resolver.PrepareStartTaskFiles(st2)
	if err != nil {
		t.Fatalf("PrepareStartTaskFiles(body): %v", err)
	}
	if filepath.Base(logPath) != storetasks.AgentLogFileName {
		t.Fatalf("log path = %q, want base %q",
			logPath, storetasks.AgentLogFileName)
	}

	// The staged requirements.md should match the body.
	reqPath := filepath.Join(
		filepath.Dir(logPath),
		storetasks.RequirementsFileName,
	)
	data, err := os.ReadFile(reqPath)
	if err != nil {
		t.Fatalf("read requirements: %v", err)
	}
	if string(data) != "body text" {
		t.Fatalf("requirements = %q, want %q",
			string(data), "body text")
	}
}

// TestRefactor_StartTargetMissingFile documents the expected
// error surface for NewStartTargetFromMarkdown when the target
// file does not exist.
func TestRefactor_StartTargetMissingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := resolver.NewStartTargetFromMarkdown("missing.md")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
