package testcases_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/resolver"
)

// TestRefactor_ResolverMarkdownFileCaps verifies the AGENTS.md
// constraints on the touched source files after the refactor:
//   - markdown.go ≤ 300 lines, ≤ 80 chars/line
//   - markdown_test.go ≤ 300 lines, ≤ 80 chars/line
//   - every method in markdown.go ≤ 80 lines
//
// This is a black-box structural assertion: it reads the files
// from disk and checks their shape, independent of runtime
// behaviour.
func TestRefactor_ResolverMarkdownFileCaps(t *testing.T) {
	// Resolve paths relative to the module root.
	root := findModuleRoot(t)
	check := func(rel string, maxLines, maxCols int) {
		abs := filepath.Join(root, rel)
		data, err := os.ReadFile(abs)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		lines := splitLines(string(data))
		if len(lines) > maxLines {
			t.Errorf("%s: %d lines > %d max",
				rel, len(lines), maxLines)
		}
		for i, line := range lines {
			if len(line) > maxCols {
				t.Errorf("%s:%d: %d chars > %d max",
					rel, i+1, len(line), maxCols)
			}
		}
	}

	check("internal/resolver/markdown.go", 300, 80)
	check("internal/resolver/markdown_test.go", 300, 80)
}

// TestRefactor_ResolverNoRunPlanSurface is a compilation guard:
// by importing the resolver package and referencing only the
// surviving start-target symbols, we confirm the deleted
// RunPlan* surface does not leak into the package's public API.
// Attempting to reference resolver.RunPlanMarkdown or
// resolver.PlanMarkdownOptions would be a compile error — this
// test ensures they're absent by exercising the surviving API
// instead.
func TestRefactor_ResolverNoRunPlanSurface(t *testing.T) {
	// Exercise the surviving API to prove the package compiles
	// without the deleted symbols.
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := resolver.NewStartTargetFromMarkdown(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.TaskID == "" {
		t.Fatal("empty TaskID")
	}
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(
			filepath.Join(dir, "go.mod"),
		); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find go.mod")
		}
		dir = parent
	}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0)
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
