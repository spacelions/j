package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeMarkdown(t *testing.T) {
	cases := []struct{ in, out string }{
		{"", ""},
		{"   \n\n", ""},
		{"# Heading line\nbody", "Heading line"},
		{"### Deep heading", "Deep heading"},
		{"plain first line\nthen heading\n# H", "plain first line"},
	}
	for _, c := range cases {
		if got := SummarizeMarkdown(c.in); got != c.out {
			t.Fatalf("SummarizeMarkdown(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

// TestSummarizeMarkdown_TruncatesRunes pins the rune-aware truncation:
// passing 90 wide-ish unicode runes must yield exactly 80 runes (the
// summaryMaxRunes constant), not 80 bytes.
func TestSummarizeMarkdown_TruncatesRunes(t *testing.T) {
	wide := strings.Repeat("é", 90)
	got := SummarizeMarkdown(wide)
	if want := strings.Repeat("é", summaryMaxRunes); got != want {
		t.Fatalf("len(runes) = %d, want %d", len([]rune(got)), summaryMaxRunes)
	}
}

// TestReadRequirementSidecar_Variants exercises the happy paths plus
// the early-return guards (empty path, empty stem).
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
	if got := ReadRequirementSidecar(filepath.Join(dir, ".plan.md")); got != "" {
		t.Fatalf("empty stem = %q", got)
	}
}

// TestReadRequirementSidecar_CandidateEqualsPlan covers the
// "candidate == planPath" guard so a non-conventional plan name does
// not loop reading the same file.
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

// TestSummary_Fallbacks pins the Summary precedence: first non-empty
// markdown line, then file basename, then empty string.
func TestSummary_Fallbacks(t *testing.T) {
	cases := []struct {
		req, target, want string
	}{
		{"# heading\nbody", "/tmp/spec.md", "heading"},
		{"", "/tmp/spec.md", "spec.md"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := Summary(c.req, c.target); got != c.want {
			t.Fatalf("Summary(%q,%q) = %q, want %q", c.req, c.target, got, c.want)
		}
	}
}

// TestPickSource returns whichever of refined-requirements / plan
// markdown yields a non-empty summary, preferring requirements.
func TestPickSource(t *testing.T) {
	cases := []struct {
		req, plan, want string
	}{
		{"# refined", "# pa", "# refined"},
		{"", "# pa", "# pa"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := PickSource(c.req, c.plan); got != c.want {
			t.Fatalf("PickSource(%q,%q) = %q, want %q", c.req, c.plan, got, c.want)
		}
	}
}

// TestFromPlanAndRequirement_Fallbacks pins the work-phase summary
// precedence: requirement first, plan body second, file basename
// last, then empty string.
func TestFromPlanAndRequirement_Fallbacks(t *testing.T) {
	cases := []struct {
		req, plan, planPath, want string
	}{
		{"# req heading\nbody", "## plan", "/tmp/x.plan.md", "req heading"},
		{"", "## plan heading", "/tmp/x.plan.md", "plan heading"},
		{"", "", "/tmp/x.plan.md", "x.plan.md"},
		{"", "", "", ""},
	}
	for _, c := range cases {
		if got := FromPlanAndRequirement(c.req, c.plan, c.planPath); got != c.want {
			t.Fatalf("FromPlanAndRequirement(%q,%q,%q) = %q, want %q", c.req, c.plan, c.planPath, got, c.want)
		}
	}
}
