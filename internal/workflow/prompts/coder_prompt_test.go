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
