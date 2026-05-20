package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

func TestStartTargetFiles(t *testing.T) {
	setupResolverProject(t)
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := NewStartTargetFromMarkdown(path)
	if err != nil {
		t.Fatalf("NewStartTargetFromMarkdown: %v", err)
	}
	logPath, err := PrepareStartTaskFiles(target)
	if err != nil {
		t.Fatalf("PrepareStartTaskFiles: %v", err)
	}
	if filepath.Base(logPath) != tasks.AgentLogFileName {
		t.Fatalf("log path = %q", logPath)
	}
	reqPath := filepath.Join(
		filepath.Dir(logPath), tasks.RequirementsFileName,
	)
	data, err := os.ReadFile(reqPath)
	if err != nil || string(data) != "body" {
		t.Fatalf("requirements = %q, %v", string(data), err)
	}
	logPath, err = PrepareStartTaskFiles(StartTarget{TaskID: "existing"})
	if err != nil || filepath.Base(logPath) != tasks.AgentLogFileName {
		t.Fatalf("existing log path = %q, %v", logPath, err)
	}
}

func TestStartTargetErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := NewStartTargetFromMarkdown("missing.md"); err == nil {
		t.Fatal("NewStartTargetFromMarkdown error = nil")
	}
	st := StartTarget{TaskID: "new", IsNew: true, Body: "body"}
	if _, err := PrepareStartTaskFiles(st); err == nil {
		t.Fatal("PrepareStartTaskFiles error = nil")
	}
}

// TestNewStartTargetFromMarkdown_UnreadableFile exercises the
// os.ReadFile error branch: mdfile.Resolve succeeds because stat
// works on a 0o000 file (it only needs x on the parent), but the
// subsequent read fails with EACCES.
func TestNewStartTargetFromMarkdown_UnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	path := filepath.Join(t.TempDir(), "locked.md")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	_, err := NewStartTargetFromMarkdown(path)
	if err == nil {
		t.Fatal("NewStartTargetFromMarkdown should fail on unreadable file")
	}
}
