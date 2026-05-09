package testcases_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/spacelions/j/internal/coding-agents/codex"
)

// TestCodexPickerListModelsWithoutBinary pins the resilience
// acceptance criterion: picking codex from the picker does not
// require an outbound API call. A workstation that is offline or
// whose codex installation is missing still gets to a usable picker.
//
// We strip PATH down to a tempdir that does NOT contain a codex
// binary and call ListModels directly. The picker uses ListModels to
// render the model list; if the implementation shelled out to the
// CLI, this would fail with exec.ErrNotFound. The test asserts the
// call succeeds and returns the static canonical alias documented in
// the package doc.
func TestCodexPickerListModelsWithoutBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	got, err := codex.New().ListModels(t.Context())
	if err != nil {
		t.Fatalf("ListModels with no codex on PATH: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("ListModels returned an empty slice")
	}

	// Canonical alias presence: "gpt-5.5" is the documented default
	// pinned in defaultModels. A future change to the alias list
	// should land here too so the picker contract stays reviewable.
	const wantCanonical = "gpt-5.5"
	if !slices.Contains(got, wantCanonical) {
		t.Fatalf(
			"ListModels = %v, missing canonical %q", got, wantCanonical,
		)
	}

	// Fresh-copy contract: callers must not be able to mutate the
	// package-level state by writing into the returned slice.
	got[0] = "MUTATED"
	again, err := codex.New().ListModels(t.Context())
	if err != nil {
		t.Fatalf("ListModels (second call): %v", err)
	}
	if reflect.DeepEqual(got, again) {
		t.Fatalf(
			"ListModels returned a shared slice — mutation leaked: %v",
			again,
		)
	}
}
