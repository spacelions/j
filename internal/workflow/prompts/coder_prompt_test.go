package prompts

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/workflow/agents/coder"
)

func TestBuildCoder(t *testing.T) {
	got := BuildCoder("/tmp/feature.plan.md", "1. step one\n2. step two")

	if !strings.Contains(got, strings.TrimSpace(coder.Instruction)) {
		t.Fatalf("prompt missing coder.Instruction: %q", got)
	}
	if !strings.Contains(got, "/tmp/feature.plan.md") {
		t.Fatalf("prompt missing plan path: %q", got)
	}
	if !strings.Contains(got, "1. step one") {
		t.Fatalf("prompt missing body: %q", got)
	}
}

// TestBuildCoder_TrimsLeadingTrailingWhitespace confirms that excess
// whitespace at the start of the embedded instruction does not bleed
// into the rendered prompt — the coder.Instruction value is trimmed
// before composition.
func TestBuildCoder_TrimsLeadingTrailingWhitespace(t *testing.T) {
	got := BuildCoder("p.md", "x")
	if strings.HasPrefix(got, "\n") || strings.HasPrefix(got, " ") {
		t.Fatalf("prompt should not start with whitespace: %q", got[:10])
	}
}

// TestBuildCoderResume pins the resume-only coder prompt (AC#5b):
// non-empty, mentions previous/check/continue, embeds the plan path
// and body, omits coder.Instruction, and differs from BuildCoder.
func TestBuildCoderResume(t *testing.T) {
	const planPath = "/tmp/feature.plan.md"
	const body = "1. step one\n2. step two"
	got := BuildCoderResume(planPath, body)
	if got == "" {
		t.Fatal("BuildCoderResume returned empty string")
	}
	lower := strings.ToLower(got)
	for _, marker := range []string{"previous", "check", "continue"} {
		if !strings.Contains(lower, marker) {
			t.Fatalf("resume prompt missing marker %q: %q", marker, got)
		}
	}
	if !strings.Contains(got, planPath) {
		t.Fatalf("resume prompt missing plan path: %q", got)
	}
	if !strings.Contains(got, "1. step one") {
		t.Fatalf("resume prompt missing body: %q", got)
	}
	if strings.Contains(got, strings.TrimSpace(coder.Instruction)) {
		t.Fatalf("resume prompt should NOT include coder.Instruction: %q", got)
	}
	if got == BuildCoder(planPath, body) {
		t.Fatal("resume prompt should differ from BuildCoder output")
	}
}
