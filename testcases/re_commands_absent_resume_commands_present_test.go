package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
)

// TestReCommands_AbsentResumeCommands_Present pins the SPA-86
// acceptance criterion that `re-plan`, `re-work`, and `re-verify` are
// removed from the CLI and replaced by the unified `resume-plan`,
// `resume-work`, and `resume-verify` commands. A caller who reads the
// cobra help output of `j tasks` must not see any `re-*` verb and must
// see all three `resume-*` verbs.
func TestReCommands_AbsentResumeCommands_Present(t *testing.T) {
	parent := tasks.New()
	subNames := make(map[string]bool)
	for _, sub := range parent.Commands() {
		subNames[sub.Name()] = true
	}

	removed := []string{"re-plan", "re-work", "re-verify"}
	for _, name := range removed {
		if subNames[name] {
			t.Errorf(
				"command %q must be removed from `j tasks` "+
					"after SPA-86 (found in subcommands)",
				name,
			)
		}
	}

	added := []string{"resume-plan", "resume-work", "resume-verify"}
	for _, name := range added {
		if !subNames[name] {
			t.Errorf(
				"command %q must be registered under `j tasks` "+
					"after SPA-86 (not found in subcommands)",
				name,
			)
		}
	}
}
