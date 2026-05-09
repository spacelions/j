package testcases_test

import (
	"regexp"
	"strings"
	"testing"
)

func TestLefthook_CommitMsg_RunsCheckCommitMessage(t *testing.T) {
	t.Parallel()

	body := readLefthookYAML(t)

	if !regexp.MustCompile(`(?m)^commit-msg:\s*$`).MatchString(body) {
		t.Fatalf("lefthook.yml: missing top-level `commit-msg:` hook")
	}

	runLine := regexp.MustCompile(
		`(?m)^\s{4}commit-message:\s*\n` +
			`(?:\s{6}.+\n)*?` +
			`\s{6}run:\s*\.hooks/check-commit-message \{1\}\s*$`)
	if !runLine.MatchString(body) {
		t.Fatalf("lefthook.yml: commit-msg must run "+
			"`.hooks/check-commit-message {1}`; got:\n%s", body)
	}

	if strings.Contains(body, "check-pr-title") {
		t.Fatalf("lefthook.yml: must not reference stale PR title hook")
	}
}
