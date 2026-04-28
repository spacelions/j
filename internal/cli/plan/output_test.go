package plan

import (
	"path/filepath"
	"testing"
)

func TestPlanOutputPath(t *testing.T) {
	cases := map[string]string{
		"/tmp/foo/spec.md":      "/tmp/foo/spec.plan.md",
		"/tmp/foo/1.md":         "/tmp/foo/1.plan.md",
		"/tmp/foo/feature.MD":   "/tmp/foo/feature.plan.md",
		"/tmp/foo/idx.markdown": "/tmp/foo/idx.plan.md",
		"/abs/no-ext":           "/abs/no-ext.plan.md",
	}
	for in, want := range cases {
		got := planOutputPath(filepath.FromSlash(in))
		if got != filepath.FromSlash(want) {
			t.Fatalf("planOutputPath(%q) = %q, want %q", in, got, want)
		}
	}
}
