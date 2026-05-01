package tasklog

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadRequirementSidecar_Variants exercises the happy paths plus
// the early-return guards (empty path, empty stem). Ported from the
// old cli/work-local `readRequirementSidecar` test.
func TestReadRequirementSidecar_Variants(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadRequirementSidecar(plan); got != "" {
		t.Fatalf("missing sidecar = %q, want empty", got)
	}
	requirement := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(requirement, []byte("req"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadRequirementSidecar(plan); got != "req" {
		t.Fatalf("present sidecar = %q, want req", got)
	}
	if got := ReadRequirementSidecar(""); got != "" {
		t.Fatalf("empty path = %q", got)
	}
	// A bare ".plan.md" path: stem is empty after stripping; the
	// helper must return "" instead of resolving to "<dir>/.md".
	if got := ReadRequirementSidecar(filepath.Join(dir, ".plan.md")); got != "" {
		t.Fatalf("empty stem = %q", got)
	}
}

// TestReadRequirementSidecar_CandidateEqualsPlan covers the
// "candidate == planPath" guard: when a non-conventional plan name
// would resolve to itself as the requirement we must not loop on
// reading the same file. Using a plan name that does NOT end in
// `.plan.md` so the trim leaves the stem alone and the candidate
// becomes identical to the input.
func TestReadRequirementSidecar_CandidateEqualsPlan(t *testing.T) {
	dir := t.TempDir()
	bare := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(bare, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadRequirementSidecar(bare); got != "" {
		t.Fatalf("self-sidecar = %q, want empty", got)
	}
}
