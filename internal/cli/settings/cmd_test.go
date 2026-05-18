package settings

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_Smoke(t *testing.T) {
	cmd := New()
	require.NotNil(t, cmd)
	assert.Equal(t, "settings", cmd.Use)
	assert.NotNil(t, cmd.RunE, "bare `j settings` would fail")

	var found struct{ set, reset bool }
	for _, sub := range cmd.Commands() {
		switch {
		case strings.HasPrefix(sub.Use, "set "):
			found.set = true
		case strings.HasPrefix(sub.Use, "reset "):
			found.reset = true
		}
	}
	assert.True(t, found.set, "missing set subcommand")
	assert.True(t, found.reset, "missing reset subcommand")
}

// TestNew_BareSettingsRunsList exercises the parent RunE: plain
// `j settings` is the list path. After mustInit the DB carries only
// the project.must_read placeholder seeded by preflight, but the four
// known section headers always render in fixed order.
func TestNew_BareSettingsRunsList(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	require.NoError(t, cmd.Execute(), "Execute")
	want := "[project]\n" +
		"  must_read = \n" +
		"\n" +
		"[planner]\n" +
		"\n" +
		"[worker]\n" +
		"\n" +
		"[verifier]\n"
	assert.Equal(t, want, stdout.String())
}
