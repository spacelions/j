package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/instructions"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
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
	dbPath := store.DefaultPath()
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
	dbPath := store.DefaultPath()
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
	dbPath := store.DefaultPath()
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
	got := BuildWorker("/tmp/plan.md", "", nil, "/tmp/c.md")
	if !strings.Contains(got, body) {
		t.Fatalf("worker override not rendered: %q", got)
	}
}

func TestBuildWorkerResume_HonoursOverride(t *testing.T) {
	const body = "WORKER RESUME OVERRIDE"
	seedPromptOverride(t, store.BucketWorker, body)
	got := BuildWorkerResume("/tmp/plan.md", "", nil, "/tmp/c.md")
	if !strings.Contains(got, body) {
		t.Fatalf("worker-resume override not rendered: %q", got)
	}
}

func TestBuildVerifier_HonoursOverride(t *testing.T) {
	const body = "VERIFIER OVERRIDE BODY"
	seedPromptOverride(t, store.BucketVerifier, body)
	got := BuildVerifier("r.md", "p.md", "vp.md", "vf.md", "", nil, "c.md")
	if !strings.Contains(got, body) {
		t.Fatalf("verifier override not rendered: %q", got)
	}
}

func TestBuildVerifierResume_HonoursOverride(t *testing.T) {
	const body = "VERIFIER RESUME OVERRIDE"
	seedPromptOverride(t, store.BucketVerifier, body)
	got := BuildVerifierResume("r.md", "p.md", "", nil, "c.md")
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
	got := BuildVerifierFix("p.md", "vf.md", "", "c.md")
	if !strings.Contains(got, body) {
		t.Fatalf("fix-loop did not honour worker override: %q", got)
	}
}

// TestPlannerOverride_StubBodyStillCarriesContracts pins the
// user-stated AC: a stub planner.md that mentions none of the
// canonical filenames or output-shape rules MUST still produce a
// composed prompt that contains every contract — they all live in
// the always-injected save suffix now.
func TestPlannerOverride_StubBodyStillCarriesContracts(t *testing.T) {
	seedPromptOverride(
		t, store.BucketPlanner, "You are a planner.\n",
	)
	const (
		req     = "/tmp/.j/tasks/abc/requirements.md"
		plan    = "/tmp/.j/tasks/abc/plan.md"
		clarify = "/tmp/.j/tasks/abc/clarification.md"
	)
	for _, base := range []string{
		AppendPlannerSaveSuffix(
			BuildPlanner(req, nil),
			tasks.TaskPaths{
				Requirements:  req,
				Plan:          plan,
				Clarification: clarify,
			},
		),
		AppendPlannerSaveSuffix(
			BuildPlannerResume(req, nil),
			tasks.TaskPaths{
				Requirements:  req,
				Plan:          plan,
				Clarification: clarify,
			},
		),
	} {
		for _, want := range []string{
			req, plan, clarify,
			"one-line summary", "PM/QA-style spec",
			"plan.md is the technical companion",
			"belong in plan.md",
		} {
			if !strings.Contains(base, want) {
				t.Fatalf("planner override prompt missing %q (the suffix should still carry it): %q", want, base)
			}
		}
	}
}

// TestWorkerOverride_StubBodyStillCarriesClarification pins the AC:
// a stub worker.md that does not mention `clarification.md` MUST
// still produce a composed prompt that names the per-task
// clarification path; the contract lives in the always-injected
// worker_plan.md tail.
func TestWorkerOverride_StubBodyStillCarriesClarification(t *testing.T) {
	seedPromptOverride(
		t, store.BucketWorker, "You are a worker.\n",
	)
	const (
		plan    = "/tmp/.j/tasks/abc/plan.md"
		clarify = "/tmp/.j/tasks/abc/clarification.md"
	)
	for _, got := range []string{
		BuildWorker(plan, "", nil, clarify),
		BuildWorkerResume(plan, "", nil, clarify),
	} {
		if !strings.Contains(got, clarify) {
			t.Fatalf("worker override prompt missing clarification path %q: %q", clarify, got)
		}
		if !strings.Contains(got, "If you need clarification") {
			t.Fatalf("worker override prompt missing escape hatch line: %q", got)
		}
	}
}

// TestVerifierOverride_StubBodyStillCarriesVerdictContract pins
// the AC: a stub verifier.md that drops the VERDICT contract MUST
// still produce a composed prompt with the contract injected via
// verifier_request.md, plus the per-task findings and clarification
// paths.
func TestVerifierOverride_StubBodyStillCarriesVerdictContract(t *testing.T) {
	seedPromptOverride(
		t, store.BucketVerifier, "You are a verifier.\n",
	)
	const (
		req      = "/tmp/.j/tasks/abc/requirements.md"
		plan     = "/tmp/.j/tasks/abc/plan.md"
		findings = "/tmp/.j/tasks/abc/verifier_findings.md"
		vplan    = "/tmp/.j/tasks/abc/verifier_plan.md"
		clarify  = "/tmp/.j/tasks/abc/clarification.md"
	)
	got := BuildVerifier(
		req, plan, vplan, findings, "", nil, clarify,
	)
	for _, want := range []string{
		req, plan, findings, clarify,
		"VERDICT: PASS", "VERDICT: FAIL",
		"last non-empty line",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("verifier override prompt missing %q (the suffix should still carry it): %q", want, got)
		}
	}
}
