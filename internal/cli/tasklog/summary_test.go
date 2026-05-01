package tasklog

import "testing"

// TestSummary_Fallbacks pins the Summary precedence: first non-empty
// markdown line, then file basename, then empty string. Replaces the
// old cli/plan-local `planSummary` test; the plan phase calls this
// helper directly so the behaviour guarantees stay here.
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
// Replaces the old cli/plan-local `pickSummarySource` test.
func TestPickSource(t *testing.T) {
	cases := []struct {
		req, plan, want string
	}{
		{"# refined", "# pa", "# refined"},
		{"", "# pa", "# pa"},
		{"", "", ""},
	}
	for _, c := range cases {
		got := PickSource(c.req, c.plan)
		if got != c.want {
			t.Fatalf("PickSource(%q,%q) = %q, want %q", c.req, c.plan, got, c.want)
		}
	}
}

// TestFromPlanAndRequirement_Fallbacks pins the work-phase summary
// precedence: requirement first, plan body second, file basename
// last, then empty string. Replaces the old cli/work-local
// `workSummary` test.
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
