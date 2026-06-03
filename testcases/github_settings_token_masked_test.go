package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

const githubToken = "ghp_TESTTOKEN"

// TestGithubSettingsTokenMasked verifies that GitHub token
// configuration is local settings state and is masked when listed.
func TestGithubSettingsTokenMasked(t *testing.T) {
	freshInit(t)

	listing, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("fresh list: %v", err)
	}
	if strings.Contains(listing, "[github]") {
		t.Fatalf("fresh init should not create github settings: %q", listing)
	}

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "github.token="+githubToken,
	); err != nil {
		t.Fatalf("set github.token: %v", err)
	}
	listing, _, err = testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listing, "[github]\n  token = ****\n") {
		t.Fatalf("listing = %q, want masked github.token", listing)
	}
	if strings.Contains(listing, githubToken) {
		t.Fatalf("listing leaked the real token: %q", listing)
	}
}
