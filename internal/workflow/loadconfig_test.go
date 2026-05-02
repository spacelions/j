package workflow

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// putProject is a one-line writer used across the loader tests.
func putProject(t *testing.T, key, value string) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Put(store.BucketProject, key, value); err != nil {
		t.Fatalf("Put %s: %v", key, err)
	}
}

// TestLoadConfig_MissingStore exercises the missing-file branch: with
// no `.j/settings` on disk, LoadConfig surfaces a wrapped fs.ErrNotExist
// whose message points the user at `j init`.
func TestLoadConfig_MissingStore(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error for missing settings store")
	}
	if !strings.Contains(err.Error(), "j init") {
		t.Fatalf("err = %v, want hint to run `j init`", err)
	}
}

// TestLoadConfig_StatNonENOENT exercises the non-ErrNotExist stat
// branch: a regular file at the .j path (instead of the expected
// directory) makes os.Stat(.j/settings) surface ENOTDIR, which is not
// fs.ErrNotExist and therefore takes the wrapped-stat-error path.
func TestLoadConfig_StatNonENOENT(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jDir, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jDir, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadConfig()
	if err == nil {
		t.Fatal("expected stat error to propagate")
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Fatalf("err = %v, want wrapped stat error", err)
	}
}

// TestLoadConfig_OpenError exercises the store.Open failure branch:
// when .j/settings is a directory, bbolt cannot open it and the
// wrapped error propagates.
func TestLoadConfig_OpenError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = LoadConfig()
	if err == nil {
		t.Fatal("expected open error")
	}
	if !strings.Contains(err.Error(), "open settings") {
		t.Fatalf("err = %v, want wrapped open error", err)
	}
}

func TestLoadConfig_MissingAPIKey(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig()
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("err = %v, want ErrMissingAPIKey", err)
	}
}

func TestLoadConfig_MissingModel(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "api_key", "k")
	_, err := LoadConfig()
	if !errors.Is(err, ErrMissingModel) {
		t.Fatalf("err = %v, want ErrMissingModel", err)
	}
}

func TestLoadConfig_MissingMaxIterations(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "api_key", "k")
	putProject(t, "model", "gemini-2.5-flash")
	_, err := LoadConfig()
	if !errors.Is(err, ErrMissingMaxIterations) {
		t.Fatalf("err = %v, want ErrMissingMaxIterations", err)
	}
}

// TestLoadConfig_UnparseableMaxIterations covers the strconv.ParseUint
// failure path: a non-numeric value is treated identically to "missing"
// so the user gets the same actionable hint.
func TestLoadConfig_UnparseableMaxIterations(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "api_key", "k")
	putProject(t, "model", "m")
	putProject(t, "max_iterations", "not-a-number")
	_, err := LoadConfig()
	if !errors.Is(err, ErrMissingMaxIterations) {
		t.Fatalf("err = %v, want ErrMissingMaxIterations", err)
	}
}

// TestLoadConfig_ZeroMaxIterations covers the "0 means missing"
// branch consistent with the legacy semantics: a literal "0" must not
// produce a Config because a zero-iteration loop has no useful
// behaviour.
func TestLoadConfig_ZeroMaxIterations(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "api_key", "k")
	putProject(t, "model", "m")
	putProject(t, "max_iterations", "0")
	_, err := LoadConfig()
	if !errors.Is(err, ErrMissingMaxIterations) {
		t.Fatalf("err = %v, want ErrMissingMaxIterations", err)
	}
}

func TestLoadConfig_Success(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "api_key", "  k  ")
	putProject(t, "model", "  gemini-2.5-flash  ")
	putProject(t, "max_iterations", "5")
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := Config{APIKey: "k", Model: "gemini-2.5-flash", MaxIterations: 5}
	if got != want {
		t.Fatalf("LoadConfig = %+v, want %+v", got, want)
	}
}
