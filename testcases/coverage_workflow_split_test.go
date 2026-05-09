package testcases_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestBranchCoverage_Workflow_RunsDedicatedTarget(t *testing.T) {
	body := readRepoFile(t, ".github", "workflows", "branch-coverage.yml")

	for _, evt := range []string{"push", "pull_request"} {
		evtRe := regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(evt) +
			`:\s*$`)
		if !evtRe.MatchString(body) {
			t.Fatalf("branch-coverage.yml: missing trigger %q", evt)
		}
	}

	run := regexp.MustCompile(`(?m)^\s+run:\s*make branch-coverage\s*$`)
	if !run.MatchString(body) {
		t.Fatalf("branch-coverage.yml: no step runs " +
			"`make branch-coverage`")
	}
}

func TestMakefile_CoverageTargets_AreSplit(t *testing.T) {
	body := readRepoFile(t, "Makefile")

	alias := regexp.MustCompile(`(?m)^coverage:\s*line-coverage\s*$`)
	if !alias.MatchString(body) {
		t.Fatalf("Makefile: `coverage` must alias `line-coverage`")
	}

	coverage := makefileTargetSection(t, body, "coverage")
	if strings.Contains(coverage, "gobco") {
		t.Fatalf("Makefile: `coverage` must not invoke gobco")
	}

	branch := makefileTargetSection(t, body, "branch-coverage")
	if !strings.Contains(branch, "go tool gobco -branch") {
		t.Fatalf("Makefile: `branch-coverage` must invoke gobco")
	}
	if strings.Contains(branch, "exit 1") {
		t.Fatalf("Makefile: `branch-coverage` must report only")
	}
}

func TestClaudeCodeReview_Workflow_IsAbsent(t *testing.T) {
	path := repoPath(t, ".github", "workflows", "claude-code-review.yml")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s should not exist; stat err = %v", path, err)
	}
}

func readRepoFile(t *testing.T, elem ...string) string {
	t.Helper()
	path := repoPath(t, elem...)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(raw)
}

func repoPath(t *testing.T, elem ...string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	parts := append([]string{filepath.Dir(filepath.Dir(thisFile))}, elem...)
	return filepath.Join(parts...)
}

func makefileTargetSection(t *testing.T, body, target string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:.*$`)
	loc := re.FindStringIndex(body)
	if loc == nil {
		t.Fatalf("Makefile: missing `%s` target", target)
	}
	rest := body[loc[1]:]
	next := regexp.MustCompile(`(?m)^[A-Za-z0-9_.-]+:`)
	nextLoc := next.FindStringIndex(rest)
	if nextLoc == nil {
		return body[loc[0]:]
	}
	return body[loc[0] : loc[1]+nextLoc[0]]
}
