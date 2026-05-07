package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

const linearAPIKey = "lin_api_TESTTOKEN"

// TestLinearSettings_SetAPIKeyMasked pins the secret-redaction
// allowlist for the Linear API key: the value is stored verbatim
// but the listing renders `api_key = ****`. The token must NEVER
// echo into stdout of `j settings`.
//
// Replaces testcases/linear-settings-set-api-key-masked.md.
func TestLinearSettings_SetAPIKeyMasked(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(settings.New(),
		"set", "linear.api_key="+linearAPIKey,
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	listing, _, err := testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listing, "[linear]\n  api_key = ****\n") {
		t.Fatalf("listing = %q, want masked api_key under [linear]", listing)
	}
	if strings.Contains(listing, linearAPIKey) {
		t.Fatalf("listing leaked the real token: %q", listing)
	}
}

// TestLinearSettings_SetAPIKeyKebabForm pins that
// `linear.api-key=<v>` (kebab) and `linear.api_key=<v>` (snake)
// both round-trip to the same bbolt key (camelCase `apiKey`) and
// surface in the listing as the kebab-display form `api_key`.
//
// Replaces testcases/linear-settings-set-api-key-kebab-form.md.
func TestLinearSettings_SetAPIKeyKebabForm(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(settings.New(),
		"set", "linear.api-key=lin_api_KEBAB",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	listing, _, err := testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listing, "[linear]\n  api_key = ****\n") {
		t.Fatalf("listing = %q, want kebab→snake api_key alias", listing)
	}
}

// TestLinearSettings_SetProjectRoundtrip pins that
// `linear.project=<v>` is NOT masked (only the API key is on the
// secret allowlist).
//
// Replaces testcases/linear-settings-set-project-roundtrip.md.
func TestLinearSettings_SetProjectRoundtrip(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(settings.New(),
		"set", "linear.project=my-default-project",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	listing, _, err := testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listing, "[linear]\n  project = my-default-project\n") {
		t.Fatalf("listing = %q, want plaintext project row", listing)
	}
}

// TestLinearSettings_ResetAPIKey pins that a single-key reset on
// `linear.api_key` removes the api_key row but preserves
// `linear.project` (single-key reset, not bucket-wide).
//
// Replaces testcases/linear-settings-reset-api-key.md.
func TestLinearSettings_ResetAPIKey(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(settings.New(),
		"set", "linear.api_key="+linearAPIKey, "linear.project=p-1",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	stdout, _, err := testutil.RunCobra(settings.New(),
		"reset", "linear.api_key",
	)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(stdout, "unset linear.api_key\n") {
		t.Fatalf("stdout = %q, want unset linear.api_key echo", stdout)
	}
	listing, _, err := testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(listing, "api_key") {
		t.Fatalf("api_key row should be gone: %q", listing)
	}
	if !strings.Contains(listing, "[linear]\n  project = p-1\n") {
		t.Fatalf("project row should survive: %q", listing)
	}
}
