package testcases_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// TestLefthook_PreCommit_RunsMakeLint pins the acceptance criterion that
// the pre-commit chain in lefthook.yml includes a lint step that runs
// `make lint`. Without this hook entry, lint failures would not block a
// local `git commit`.
func TestLefthook_PreCommit_RunsMakeLint(t *testing.T) {
	body := readLefthookYAML(t)

	// The lint command must exist as a sub-key under pre-commit.commands.
	if !regexp.MustCompile(`(?m)^\s{4}lint:\s*$`).MatchString(body) {
		t.Fatalf("lefthook.yml: missing `lint:` command under " +
			"pre-commit.commands")
	}

	// Its run line must invoke `make lint` (not lint-fix or anything else).
	runLine := regexp.MustCompile(
		`(?m)^\s{4}lint:\s*\n(?:\s{6}.+\n)*?\s{6}run:\s*make lint\s*$`)
	if !runLine.MatchString(body) {
		t.Fatalf("lefthook.yml: `lint:` command must `run: make lint`; "+
			"got:\n%s", body)
	}
}

// readLefthookYAML reads <repoRoot>/lefthook.yml as a string.
func readLefthookYAML(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(thisFile))
	path := filepath.Join(repoRoot, "lefthook.yml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(body)
}
