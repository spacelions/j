package plan

import (
	"strings"
	"testing"
)

func TestPlanSource_String(t *testing.T) {
	cases := []struct {
		s    PlanSource
		want string
	}{
		{SourceMarkdown, "markdown"},
		{SourceLinear, "linear"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("PlanSource(%d).String() = %q, want %q", int(tc.s), got, tc.want)
		}
	}
	if got := PlanSource(99).String(); !strings.Contains(got, "PlanSource(99)") {
		t.Errorf("unknown PlanSource fallback = %q", got)
	}
}

func TestParseSource(t *testing.T) {
	cases := []struct {
		in   string
		want PlanSource
	}{
		{"markdown", SourceMarkdown},
		{"linear", SourceLinear},
	}
	for _, tc := range cases {
		got, err := ParseSource(tc.in)
		if err != nil {
			t.Errorf("ParseSource(%q) err = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSource(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := ParseSource("nonsense"); err == nil {
		t.Error("ParseSource should fail on unknown labels")
	}
	if _, err := ParseSource("from scratch"); err == nil {
		t.Error("ParseSource should reject the removed scratch label")
	}
	// The previous "markdown file" spelling is rejected explicitly
	// so a stale config surfaces a clear "unknown source" error
	// rather than silently continuing to work.
	if _, err := ParseSource("markdown file"); err == nil {
		t.Error("ParseSource should reject the old markdown-file label")
	}
}

// TestSourceLabels_RoundTrip guarantees every label shown by the
// picker round-trips through ParseSource.
func TestSourceLabels_RoundTrip(t *testing.T) {
	for _, label := range SourceLabels {
		s, err := ParseSource(label)
		if err != nil {
			t.Errorf("label %q failed to parse: %v", label, err)
			continue
		}
		if s.String() != label {
			t.Errorf("round-trip mismatch: %q -> %v -> %q", label, s, s.String())
		}
	}
}
