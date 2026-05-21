package linear

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// initLinearProject lays down the .j layout in a fresh temp dir.
// The linear package's tests stay independent of the cli/preflight
// must_read seed (and of internal/testutil) so this helper just
// invokes store.EnsureProject directly.
func initLinearProject(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
}

func TestSaveAndLoadAPIKey_RoundTrip(t *testing.T) {
	initLinearProject(t)
	if err := SaveAPIKey("lin_api_secret"); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
	got, err := LoadAPIKey()
	if err != nil {
		t.Fatalf("LoadAPIKey: %v", err)
	}
	if got != "lin_api_secret" {
		t.Fatalf("LoadAPIKey = %q", got)
	}
}

func TestSaveAndLoadProject_RoundTrip(t *testing.T) {
	initLinearProject(t)
	if err := SaveProject("project-id"); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}
	got, err := LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if got != "project-id" {
		t.Fatalf("LoadProject = %q", got)
	}
}

func TestLoadAPIKey_MissingSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	got, err := LoadAPIKey()
	if err != nil {
		t.Fatalf("LoadAPIKey on fresh cwd: err = %v", err)
	}
	if got != "" {
		t.Fatalf("LoadAPIKey = %q, want empty for missing settings", got)
	}
}

func TestLoadProject_MissingSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	got, err := LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if got != "" {
		t.Fatalf("LoadProject = %q, want empty", got)
	}
}

func TestLoadAPIKey_StatNonENOENT(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := store.DefaultDir()
	// A non-directory at the .j path makes the settings stat path fail
	// on a non-ErrNotExist error. The settings DB lives at .j/settings.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAPIKey(); err == nil {
		t.Fatal("LoadAPIKey on non-file settings: err = nil, want failure")
	}
}

func TestSaveAPIKey_OpenError(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := store.DefaultDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey("k"); err == nil {
		t.Fatal("SaveAPIKey on bad layout: err = nil, want failure")
	}
}
