package testcases_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommitMessageValidator_AcceptsValidMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "feature message",
			message: "feat(githooks)[SPA-81]: enforce commit message format",
		},
		{
			name:    "chore message",
			message: "chore(ci)[SPA-82]: update workflow",
		},
		{
			name:    "build message",
			message: "build(deps)[SPA-83]: bump tools",
		},
		{
			name:    "fix message",
			message: "fix(tasks)[SPA-84]: handle completion",
		},
		{
			name:    "style message",
			message: "style(cli)[SPA-85]: format output",
		},
		{
			name:    "docs message",
			message: "docs(readme)[SPA-86]: clarify setup",
		},
		{
			name:    "refactor message",
			message: "refactor(store)[SPA-87]: simplify lookups",
		},
		{
			name: "message with body",
			message: "fix(hooks)[SPA-88]: validate subject\n\n" +
				"Body text is ignored by the commit-msg hook.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output, err := runCommitMessageValidator(t, tt.message)
			require.NoErrorf(t, err, "check-commit-message failed:\n%s", output)
		})
	}
}

func TestCommitMessageValidator_RejectsInvalidMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "invalid type",
			message: "test(githooks)[SPA-81]: enforce commit format",
		},
		{
			name:    "missing component",
			message: "feat()[SPA-81]: enforce commit format",
		},
		{
			name:    "missing spa number",
			message: "feat(githooks): enforce commit format",
		},
		{
			name:    "missing title",
			message: "feat(githooks)[SPA-81]: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output, err := runCommitMessageValidator(t, tt.message)
			require.Errorf(t, err, "check-commit-message passed:\n%s", output)
			assert.Contains(t, output, "required format:")
			required := "<type>(<component>)[SPA-<number>]: <title>"
			assert.Contains(t, output, required)
		})
	}
}

func runCommitMessageValidator(t *testing.T, message string) (string, error) {
	t.Helper()

	messageFile := filepath.Join(t.TempDir(), "COMMIT_EDITMSG")
	require.NoError(t, os.WriteFile(messageFile, []byte(message), 0o600))

	script := filepath.Join(
		repoPath(t),
		".hooks",
		"check-commit-message",
	)
	cmd := exec.CommandContext(t.Context(), script, messageFile)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
