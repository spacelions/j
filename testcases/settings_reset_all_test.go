package testcases_test

import (
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
)

// TestSettingsResetAll_HeadlessYesFlag pins the `--yes` short-circuit:
// no confirmation prompt is read, the entire `.j/` directory is wiped
// (not just the settings DB — `runResetFull` calls os.RemoveAll(jDir)),
// and stdout reports `J: removed <abs-path>`.
//
// Replaces testcases/manual/settings-reset-all.md (headless variant).
func TestSettingsResetAll_HeadlessYesFlag(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "plan.tool=cursor", "plan.model=sonnet-4",
	); err != nil {
		t.Fatalf("set: %v", err)
	}

	jDir := store.DefaultDir()
	stdout, _, err := testutil.RunCobra(t, settings.New(), "reset", "--yes")
	if err != nil {
		t.Fatalf("reset --yes: %v", err)
	}
	if !strings.Contains(stdout, "removed "+jDir) {
		t.Fatalf("stdout = %q, want `removed %s`", stdout, jDir)
	}
	if _, err := os.Stat(jDir); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be gone, got err=%v", jDir, err)
	}
}

// TestSettingsResetAll_InteractiveDecline pins the no-`--yes` flow
// where the user types `n` + Enter at the confirmation: exit 0,
// stdout says `reset aborted`, `.j/` still exists. The reset path
// reads stdin via `bufio.NewReader(cmd.InOrStdin())` so wiring a
// `strings.NewReader` into the cobra cmd is enough to drive it (no
// TTY required).
//
// Replaces testcases/manual/settings-reset-all.md (interactive
// decline variant).
func TestSettingsResetAll_InteractiveDecline(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "plan.tool=cursor",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	jDir := store.DefaultDir()

	cmd := settings.New()
	cmd.SetIn(strings.NewReader("n\n"))
	stdout, _, err := runWithStdin(t, cmd, "reset")
	if err != nil {
		t.Fatalf("reset (decline): %v", err)
	}
	if !strings.Contains(stdout, "reset aborted") {
		t.Fatalf("stdout = %q, want `reset aborted`", stdout)
	}
	if _, err := os.Stat(jDir); err != nil {
		t.Fatalf("%s should still exist after decline: err=%v", jDir, err)
	}
	listing, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("post-decline list: %v", err)
	}
	if !strings.Contains(listing, "[plan]\n  tool = cursor\n") {
		t.Fatalf("decline must not mutate state: %q", listing)
	}
}

// TestSettingsResetAll_InteractiveAccept pins the no-`--yes` flow
// where the user types `y` + Enter: exit 0, stdout says
// `removed <abs-path>`, `.j/` is gone.
//
// Replaces testcases/manual/settings-reset-all.md (interactive
// accept variant).
func TestSettingsResetAll_InteractiveAccept(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "plan.tool=cursor",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	jDir := store.DefaultDir()

	cmd := settings.New()
	cmd.SetIn(strings.NewReader("y\n"))
	stdout, _, err := runWithStdin(t, cmd, "reset")
	if err != nil {
		t.Fatalf("reset (accept): %v", err)
	}
	if !strings.Contains(stdout, "removed "+jDir) {
		t.Fatalf("stdout = %q, want `removed %s`", stdout, jDir)
	}
	if _, err := os.Stat(jDir); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be gone, got err=%v", jDir, err)
	}
}
