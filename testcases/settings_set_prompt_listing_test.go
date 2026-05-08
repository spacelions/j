package testcases_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

// TestSettingsSet_PromptListingShowsPath pins AC#3: `j settings`
// renders `prompt = <path>` under the relevant role bucket after a
// successful set.
func TestSettingsSet_PromptListingShowsPath(t *testing.T) {
	freshInit(t)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	plannerDest := filepath.Join(dir, "p.md")
	workerDest := filepath.Join(dir, "w.md")
	verifierDest := filepath.Join(dir, "v.md")

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set",
		"planner.prompt="+plannerDest,
		"worker.prompt="+workerDest,
		"verifier.prompt="+verifierDest,
	); err != nil {
		t.Fatalf("set: %v", err)
	}

	listing, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{
		"[planner]\n  prompt = " + plannerDest + "\n",
		"[worker]\n  prompt = " + workerDest + "\n",
		"[verifier]\n  prompt = " + verifierDest + "\n",
	} {
		if !strings.Contains(listing, want) {
			t.Fatalf("listing = %q missing %q", listing, want)
		}
	}
}
