package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/instructions"
	"github.com/spacelions/j/internal/store"
)

func TestResolve_UnsetReturnsEmbedded(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := Resolve(store.BucketPlanner); got != instructions.Planner {
		t.Fatalf("planner unset: got %q, want embedded", got)
	}
	if got := Resolve(store.BucketWorker); got != instructions.Worker {
		t.Fatalf("worker unset: got %q, want embedded", got)
	}
	if got := Resolve(store.BucketVerifier); got != instructions.Verifier {
		t.Fatalf("verifier unset: got %q, want embedded", got)
	}
}

func TestResolve_UnknownRoleReturnsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := Resolve("nope"); got != "" {
		t.Fatalf("unknown role: got %q, want empty string", got)
	}
}

func TestResolve_FileExistsReturnsContents(t *testing.T) {
	const body = "custom planner body\n"
	override := seedPromptOverride(t, store.BucketPlanner, body)
	if got := Resolve(store.BucketPlanner); got != body {
		t.Fatalf("planner override at %q: got %q, want %q",
			override, got, body)
	}
}

func TestResolve_MissingFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	missing := filepath.Join(dir, "does", "not", "exist.md")
	putPromptOverride(t, store.BucketWorker, missing)
	if got := Resolve(store.BucketWorker); got != instructions.Worker {
		t.Fatalf("missing override: got %q, want embedded worker", got)
	}
}

func TestResolve_RelativePathResolvedAgainstCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const rel = "rel_verifier.md"
	const body = "verifier override body"
	if err := os.WriteFile(
		filepath.Join(dir, rel), []byte(body), 0o644,
	); err != nil {
		t.Fatalf("write override: %v", err)
	}
	putPromptOverride(t, store.BucketVerifier, rel)
	if got := Resolve(store.BucketVerifier); got != body {
		t.Fatalf("relative override: got %q, want %q", got, body)
	}
}

func TestResolve_StoreMissingFallsBack(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := Resolve(store.BucketWorker); got != instructions.Worker {
		t.Fatalf("no .j layout: got %q, want embedded worker", got)
	}
}

func TestResolve_EmptyValueFallsBack(t *testing.T) {
	t.Chdir(t.TempDir())
	putPromptOverride(t, store.BucketPlanner, "")
	if got := Resolve(store.BucketPlanner); got != instructions.Planner {
		t.Fatalf("empty override: got %q, want embedded planner", got)
	}
}

// TestResolve_OpenFailsFallsBack pins the lookupPromptPath fallback
// when the on-disk DB exists but cannot be opened (e.g. it is a
// directory). The workflow must continue with the embedded default
// rather than propagating the open error.
func TestResolve_OpenFailsFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	dbPath, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if got := Resolve(store.BucketPlanner); got != instructions.Planner {
		t.Fatalf("open-fail fallback: got %q, want embedded planner", got)
	}
}

func TestEmbeddedDefault(t *testing.T) {
	cases := map[string]string{
		store.BucketPlanner:  instructions.Planner,
		store.BucketWorker:   instructions.Worker,
		store.BucketVerifier: instructions.Verifier,
	}
	for role, want := range cases {
		if got := EmbeddedDefault(role); got != want {
			t.Fatalf("EmbeddedDefault(%q) mismatch", role)
		}
	}
	if got := EmbeddedDefault("unknown"); got != "" {
		t.Fatalf("unknown role: got %q, want empty", got)
	}
}

// putPromptOverride seeds the local settings DB with
// `<bucket>.prompt=<path>` in the current working directory. The DB
// layout is created via store.EnsureProject so Open succeeds. Tests
// must call this helper after t.Chdir.
func putPromptOverride(t *testing.T, bucket, path string) {
	t.Helper()
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	dbPath, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Put(bucket, store.KeyPromptPath, path); err != nil {
		_ = s.Close()
		t.Fatalf("Put: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// seedPromptOverride lays down `<cwd>/.j/settings` and writes
// `<bucket>.prompt=<file>` after seeding the override file with
// `body`. Returns the absolute path of the override file.
func seedPromptOverride(t *testing.T, bucket, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	override := filepath.Join(dir, bucket+"_override.md")
	if err := os.WriteFile(override, []byte(body), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	dbPath, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Put(bucket, store.KeyPromptPath, override); err != nil {
		_ = s.Close()
		t.Fatalf("Put: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return override
}

func TestBuildPlanner_HonoursOverride(t *testing.T) {
	const body = "PLANNER OVERRIDE BODY"
	seedPromptOverride(t, store.BucketPlanner, body)
	got := BuildPlanner("/tmp/feature.md", nil)
	if !strings.Contains(got, body) {
		t.Fatalf("planner override not rendered: %q", got)
	}
}

func TestBuildPlannerResume_HonoursOverride(t *testing.T) {
	const body = "PLANNER RESUME OVERRIDE"
	seedPromptOverride(t, store.BucketPlanner, body)
	got := BuildPlannerResume("/tmp/feature.md", nil)
	if !strings.Contains(got, body) {
		t.Fatalf("planner-resume override not rendered: %q", got)
	}
}

func TestBuildWorker_HonoursOverride(t *testing.T) {
	const body = "WORKER OVERRIDE BODY"
	seedPromptOverride(t, store.BucketWorker, body)
	got := BuildWorker("/tmp/plan.md", "", nil)
	if !strings.Contains(got, body) {
		t.Fatalf("worker override not rendered: %q", got)
	}
}

func TestBuildWorkerResume_HonoursOverride(t *testing.T) {
	const body = "WORKER RESUME OVERRIDE"
	seedPromptOverride(t, store.BucketWorker, body)
	got := BuildWorkerResume("/tmp/plan.md", "", nil)
	if !strings.Contains(got, body) {
		t.Fatalf("worker-resume override not rendered: %q", got)
	}
}

func TestBuildVerifier_HonoursOverride(t *testing.T) {
	const body = "VERIFIER OVERRIDE BODY"
	seedPromptOverride(t, store.BucketVerifier, body)
	got := BuildVerifier("r.md", "p.md", "vp.md", "vf.md", "", nil)
	if !strings.Contains(got, body) {
		t.Fatalf("verifier override not rendered: %q", got)
	}
}

func TestBuildVerifierResume_HonoursOverride(t *testing.T) {
	const body = "VERIFIER RESUME OVERRIDE"
	seedPromptOverride(t, store.BucketVerifier, body)
	got := BuildVerifierResume("r.md", "p.md", "", nil)
	if !strings.Contains(got, body) {
		t.Fatalf("verifier-resume override not rendered: %q", got)
	}
}

// TestBuildVerifierFix_HonoursWorkerOverride pins the AC: the
// fix-loop builder runs the worker, so it must honour the worker
// override (NOT the verifier one).
func TestBuildVerifierFix_HonoursWorkerOverride(t *testing.T) {
	const body = "WORKER FIX OVERRIDE"
	seedPromptOverride(t, store.BucketWorker, body)
	got := BuildVerifierFix("p.md", "vf.md", "")
	if !strings.Contains(got, body) {
		t.Fatalf("fix-loop did not honour worker override: %q", got)
	}
}
