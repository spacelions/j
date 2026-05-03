package store

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

// putProject is the shared one-line writer used across the
// LoadProjectConfig / LoadTaskConfig tests. The settings store has
// already been laid down by EnsureProject / Init at the call site;
// this helper just opens, puts, closes.
func putProject(t *testing.T, key, value string) {
	t.Helper()
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Put(BucketProject, key, value); err != nil {
		t.Fatalf("Put %s: %v", key, err)
	}
}

// TestOpenSettings_Success pins the happy path: an initialised
// project produces a usable Store and ok=true with no warning.
func TestOpenSettings_Success(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	s, ok := OpenSettings(&stderr)
	if !ok {
		t.Fatalf("OpenSettings ok=false on initialised project; stderr=%q", stderr.String())
	}
	defer func() { _ = s.Close() }()
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be silent on success, got %q", stderr.String())
	}
}

// TestOpenSettings_OpenFailure pins the warn-and-return-false branch:
// pointing the per-project layout at a directory makes bolt.Open fail
// and OpenSettings surfaces a "warning: settings db" line on stderr.
func TestOpenSettings_OpenFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	s, ok := OpenSettings(&stderr)
	if ok {
		t.Fatalf("OpenSettings should fail on directory-shaped settings; got ok=true")
	}
	if s != nil {
		t.Fatal("OpenSettings should return nil on failure")
	}
	if !strings.Contains(stderr.String(), "warning: settings") {
		t.Fatalf("stderr should warn about settings open: %q", stderr.String())
	}
}

// TestLoadProjectConfig_MissingStore exercises the missing-file
// branch: with no `.j/settings` on disk, LoadProjectConfig surfaces
// a wrapped fs.ErrNotExist whose message points the user at `j init`.
func TestLoadProjectConfig_MissingStore(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := LoadProjectConfig()
	if err == nil {
		t.Fatal("expected error for missing settings store")
	}
	if !strings.Contains(err.Error(), "j init") {
		t.Fatalf("err = %v, want hint to run `j init`", err)
	}
}

// TestLoadProjectConfig_StatNonENOENT exercises the non-ErrNotExist
// stat branch: a regular file at the .j path (instead of the
// expected directory) makes os.Stat(.j/settings) surface ENOTDIR,
// which is not fs.ErrNotExist and therefore takes the wrapped-stat
// path.
func TestLoadProjectConfig_StatNonENOENT(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jDir, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jDir, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadProjectConfig()
	if err == nil {
		t.Fatal("expected stat error to propagate")
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Fatalf("err = %v, want wrapped stat error", err)
	}
}

// TestLoadProjectConfig_OpenError exercises the Open failure branch:
// when .j/settings is a directory, bbolt cannot open it and the
// wrapped error propagates.
func TestLoadProjectConfig_OpenError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = LoadProjectConfig()
	if err == nil {
		t.Fatal("expected open error")
	}
	if !strings.Contains(err.Error(), "open settings") {
		t.Fatalf("err = %v, want wrapped open error", err)
	}
}

func TestLoadProjectConfig_MissingAPIKey(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProjectConfig()
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("err = %v, want ErrMissingAPIKey", err)
	}
}

func TestLoadProjectConfig_MissingModel(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "api_key", "k")
	_, err := LoadProjectConfig()
	if !errors.Is(err, ErrMissingModel) {
		t.Fatalf("err = %v, want ErrMissingModel", err)
	}
}

func TestLoadProjectConfig_MissingMaxIterations(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "api_key", "k")
	putProject(t, "model", "gemini-2.5-flash")
	_, err := LoadProjectConfig()
	if !errors.Is(err, ErrMissingMaxIterations) {
		t.Fatalf("err = %v, want ErrMissingMaxIterations", err)
	}
}

// TestLoadProjectConfig_UnparseableMaxIterations covers the
// strconv.ParseUint failure path: a non-numeric value is treated
// identically to "missing" so the user gets the same actionable
// hint.
func TestLoadProjectConfig_UnparseableMaxIterations(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "api_key", "k")
	putProject(t, "model", "m")
	putProject(t, "max_iterations", "not-a-number")
	_, err := LoadProjectConfig()
	if !errors.Is(err, ErrMissingMaxIterations) {
		t.Fatalf("err = %v, want ErrMissingMaxIterations", err)
	}
}

// TestLoadProjectConfig_ZeroMaxIterations covers the "0 means
// missing" branch consistent with the legacy semantics: a literal
// "0" must not produce a Config because a zero-iteration loop has
// no useful behaviour.
func TestLoadProjectConfig_ZeroMaxIterations(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "api_key", "k")
	putProject(t, "model", "m")
	putProject(t, "max_iterations", "0")
	_, err := LoadProjectConfig()
	if !errors.Is(err, ErrMissingMaxIterations) {
		t.Fatalf("err = %v, want ErrMissingMaxIterations", err)
	}
}

func TestLoadProjectConfig_Success(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "api_key", "  k  ")
	putProject(t, "model", "  gemini-2.5-flash  ")
	putProject(t, "max_iterations", "5")
	got, err := LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	want := ProjectConfig{APIKey: "k", Model: "gemini-2.5-flash", MaxIterations: 5}
	if got != want {
		t.Fatalf("LoadProjectConfig = %+v, want %+v", got, want)
	}
}

// TestLoadTaskConfig_DefaultsWhenNoSettings pins that a fresh
// project (no .j layout at all) yields the documented default
// MaxIterations=DefaultTaskMaxIterations with no error so
// `j tasks start` can run end to end without project knobs.
func TestLoadTaskConfig_DefaultsWhenNoSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	got, err := LoadTaskConfig()
	if err != nil {
		t.Fatalf("LoadTaskConfig: %v", err)
	}
	if got.MaxIterations != DefaultTaskMaxIterations {
		t.Fatalf("MaxIterations = %d, want %d", got.MaxIterations, DefaultTaskMaxIterations)
	}
}

// TestLoadTaskConfig_DefaultsWhenSettingMissing pins the
// initialised-but-no-key branch.
func TestLoadTaskConfig_DefaultsWhenSettingMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTaskConfig()
	if err != nil {
		t.Fatalf("LoadTaskConfig: %v", err)
	}
	if got.MaxIterations != DefaultTaskMaxIterations {
		t.Fatalf("MaxIterations = %d, want %d", got.MaxIterations, DefaultTaskMaxIterations)
	}
}

// TestLoadTaskConfig_ParsesValue pins the read-and-parse path.
func TestLoadTaskConfig_ParsesValue(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "max_iterations", "5")
	got, err := LoadTaskConfig()
	if err != nil {
		t.Fatalf("LoadTaskConfig: %v", err)
	}
	if got.MaxIterations != 5 {
		t.Fatalf("MaxIterations = %d, want 5", got.MaxIterations)
	}
}

// TestLoadTaskConfig_DefaultsOnUnparseable pins that bogus values
// (and "0" sentinel) fall back to the default rather than surfacing
// as an error — we don't want the orchestrator path to break because
// of stale settings.
func TestLoadTaskConfig_DefaultsOnUnparseable(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putProject(t, "max_iterations", "not-a-number")
	got, err := LoadTaskConfig()
	if err != nil {
		t.Fatalf("LoadTaskConfig: %v", err)
	}
	if got.MaxIterations != DefaultTaskMaxIterations {
		t.Fatalf("MaxIterations = %d, want %d (unparseable fallback)",
			got.MaxIterations, DefaultTaskMaxIterations)
	}

	putProject(t, "max_iterations", "0")
	got, err = LoadTaskConfig()
	if err != nil {
		t.Fatalf("LoadTaskConfig zero: %v", err)
	}
	if got.MaxIterations != DefaultTaskMaxIterations {
		t.Fatalf("zero-value MaxIterations = %d, want %d", got.MaxIterations, DefaultTaskMaxIterations)
	}
}

// TestLoadTaskConfig_StatErrorPropagates pins the non-ENOENT
// stat-error branch.
func TestLoadTaskConfig_StatErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jDir, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jDir, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadTaskConfig()
	if err == nil || !strings.Contains(err.Error(), "stat") {
		t.Fatalf("err = %v, want wrapped stat error", err)
	}
}

// TestPersistAgentSelection_NilStore exercises the nil-store
// silent-no-op branch.
func TestPersistAgentSelection_NilStore(t *testing.T) {
	var stderr bytes.Buffer
	PersistAgentSelection(nil, &stderr, BucketPlanner, "cursor", "sonnet-4", true)
	if stderr.Len() != 0 {
		t.Fatalf("nil store should be silent, got %q", stderr.String())
	}
}

// TestPersistAgentSelection_HappyPath pins the three-key write.
func TestPersistAgentSelection_HappyPath(t *testing.T) {
	s := openInTemp(t)
	if err := s.EnsureBucket(BucketWorker); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	PersistAgentSelection(s, &stderr, BucketWorker, "cursor", "sonnet-4", false)
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	for k, want := range map[string]string{
		"tool":        "cursor",
		"model":       "sonnet-4",
		"interactive": "false",
	} {
		got, ok, err := s.Get(BucketWorker, k)
		if err != nil || !ok || got != want {
			t.Fatalf("Get(%s) = (%q,%v,%v) want %q", k, got, ok, err, want)
		}
	}
}

// TestPersistAgentSelection_StopsOnFirstError pins both halves of
// the warn-on-error branch: a closed store fails the first Put, the
// helper writes exactly one "warning: persist <key>:..." line and
// short-circuits before attempting subsequent writes.
func TestPersistAgentSelection_StopsOnFirstError(t *testing.T) {
	s := openInTemp(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	PersistAgentSelection(s, &stderr, BucketPlanner, "cursor", "sonnet-4", true)
	if !strings.Contains(stderr.String(), "warning: persist tool") {
		t.Fatalf("stderr = %q, want warning naming the failing key", stderr.String())
	}
	if got := strings.Count(stderr.String(), "warning: persist"); got != 1 {
		t.Fatalf("warning count = %d, want 1 (loop must short-circuit); stderr=%q", got, stderr.String())
	}
}
