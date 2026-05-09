package testcases_test

import (
	"regexp"
	"strings"
	"testing"
)

func TestCIWorkflow_RunsCommitMessageValidation(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, ".github", "workflows", "ci.yml")

	for _, event := range []string{"push", "pull_request"} {
		eventRe := regexp.MustCompile(`(?m)^\s+` +
			regexp.QuoteMeta(event) + `:\s*$`)
		if !eventRe.MatchString(body) {
			t.Fatalf("ci.yml: missing trigger %q", event)
		}
	}

	for _, want := range []string{
		"fetch-depth: 0",
		"name: commit-message",
		"zero_sha=\"0000000000000000000000000000000000000000\"",
		"mapfile -t subjects < <(git log --format=%s \"$range\")",
		".hooks/check-commit-message <(printf '%s\\n' \"$subject\")",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("ci.yml: missing %q:\n%s", want, body)
		}
	}

	for _, stale := range []string{
		"check-pr-title",
		"pull_request.title",
	} {
		if strings.Contains(body, stale) {
			t.Fatalf("ci.yml: still contains stale PR title check %q",
				stale)
		}
	}
}
