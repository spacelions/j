package testcases_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// TestLefthook_PreCommit_ShortCircuits pins the acceptance criterion
// that the pre-commit chain still short-circuits on the first failure.
// Lefthook implements that by setting `piped: true` on the hook; the
// hook would otherwise run all commands in parallel and aggregate
// failures, which would defeat the "block early on lint" goal.
func TestLefthook_PreCommit_ShortCircuits(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(thisFile))
	path := filepath.Join(repoRoot, "lefthook.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	body := string(raw)

	preCommit := regexp.MustCompile(`(?m)^pre-commit:\s*$`)
	if !preCommit.MatchString(body) {
		t.Fatalf("lefthook.yml: missing top-level `pre-commit:` hook")
	}

	// Within the pre-commit block (top-level entry), `piped: true` must
	// appear before the `commands:` block so lefthook short-circuits.
	piped := regexp.MustCompile(
		`(?ms)^pre-commit:\s*\n(?:\s{2}.+\n)*?\s{2}piped:\s*true\s*$`)
	if !piped.MatchString(body) {
		t.Fatalf("lefthook.yml: pre-commit must declare `piped: true` "+
			"so the chain short-circuits on first failure; got:\n%s",
			body)
	}
}
