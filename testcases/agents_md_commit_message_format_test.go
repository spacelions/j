package testcases_test

import (
	"strings"
	"testing"
)

func TestAGENTSMd_DocumentsCommitMessageFormat(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, "AGENTS.md")

	for _, want := range []string{
		"Commit messages must follow:",
		"`<type>(<component>)[SPA-<number>]: title`",
		"`feat`, `chore`, `build`, `fix`, `style`, `docs`, or `refactor`",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("AGENTS.md: missing %q", want)
		}
	}
}
