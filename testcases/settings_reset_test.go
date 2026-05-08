package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

// TestSettingsReset_Key pins the single-key reset path: the rest of
// the bucket survives the reset.
//
// Replaces testcases/settings-reset-key.md.
func TestSettingsReset_Key(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "plan.tool=cursor",
	); err != nil {
		t.Fatalf("set tool: %v", err)
	}
	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "plan.model=sonnet-4",
	); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if _, _, err := testutil.RunCobra(t, settings.New(),
		"reset", "plan.tool",
	); err != nil {
		t.Fatalf("reset: %v", err)
	}
	listing, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listing, "[plan]\n  model = sonnet-4\n") {
		t.Fatalf("listing = %q, want [plan] with only model row", listing)
	}
	if strings.Contains(listing, "tool = cursor") {
		t.Fatalf("plan.tool was not unset: %q", listing)
	}
}

// TestSettingsReset_Bucket pins the bucket-level reset: a single
// `J: unset planner` echo and an empty [planner] section in the
// follow-up listing. Re-running is a no-op success.
//
// Replaces testcases/settings-reset-bucket.md.
func TestSettingsReset_Bucket(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "planner.tool=cursor", "planner.model=opus",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	stdout, _, err := testutil.RunCobra(t, settings.New(), "reset", "planner")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(stdout, "unset planner\n") {
		t.Fatalf("stdout = %q, want one `unset planner` line", stdout)
	}
	if strings.Count(stdout, "unset planner") != 1 {
		t.Fatalf("expected exactly one unset line, got %q", stdout)
	}
	listing, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Empty [planner] section: header on its own line, no rows.
	if !strings.Contains(listing, "\n[planner]\n\n[worker]") {
		t.Fatalf("listing = %q, want empty [planner] between [project] and [worker]", listing)
	}
	// Re-running is a no-op success.
	stdout, _, err = testutil.RunCobra(t, settings.New(), "reset", "planner")
	if err != nil {
		t.Fatalf("reset (second): %v", err)
	}
	if !strings.Contains(stdout, "unset planner\n") {
		t.Fatalf("second stdout = %q, want unset planner", stdout)
	}
}

// TestSettingsReset_BucketMissingNoop pins that resetting a
// never-created bucket (or a key under one) is a no-op success.
//
// Replaces testcases/settings-reset-bucket-missing-noop.md.
func TestSettingsReset_BucketMissingNoop(t *testing.T) {
	freshInit(t)

	stdout, _, err := testutil.RunCobra(t, settings.New(), "reset", "planner")
	if err != nil {
		t.Fatalf("reset planner: %v", err)
	}
	if !strings.Contains(stdout, "unset planner\n") {
		t.Fatalf("stdout = %q, want `unset planner`", stdout)
	}
	stdout, _, err = testutil.RunCobra(t, settings.New(), "reset", "bucket.ghost")
	if err != nil {
		t.Fatalf("reset bucket.ghost: %v", err)
	}
	if !strings.Contains(stdout, "unset bucket.ghost\n") {
		t.Fatalf("stdout = %q, want `unset bucket.ghost`", stdout)
	}
}

// TestSettingsReset_MultiArgs pins the bucket+key+bucket reset
// invocation: one echo per positional arg, in left-to-right order.
//
// Replaces testcases/settings-reset-multi-args.md.
func TestSettingsReset_MultiArgs(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set",
		"planner.tool=cursor", "planner.model=opus",
		"worker.model=sonnet",
		"verifier.tool=cursor",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	stdout, _, err := testutil.RunCobra(t, settings.New(),
		"reset", "planner", "worker.model", "verifier",
	)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	for _, want := range []string{
		"unset planner\n",
		"unset worker.model\n",
		"unset verifier\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, missing %q", stdout, want)
		}
	}
	// Order: planner before worker.model before verifier.
	pp := strings.Index(stdout, "unset planner")
	pw := strings.Index(stdout, "unset worker.model")
	pv := strings.Index(stdout, "unset verifier")
	if pp >= pw || pw >= pv {
		t.Fatalf("echo lines out of order: %q", stdout)
	}
	listing, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(listing, "tool = cursor") || strings.Contains(listing, "model = opus") ||
		strings.Contains(listing, "model = sonnet") {
		t.Fatalf("residual rows: %q", listing)
	}
}

// TestSettingsReset_CommaNotSeparator pins the parser contract:
// whitespace is the only separator between targets. A literal comma
// becomes part of the bucket name and the target turns into a no-op
// because no such bucket exists.
//
// Replaces testcases/settings-reset-comma-not-separator.md.
func TestSettingsReset_CommaNotSeparator(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "planner.tool=cursor", "worker.model=sonnet",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	stdout, _, err := testutil.RunCobra(t, settings.New(),
		"reset", "planner,worker.model",
	)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(stdout, "unset planner,worker.model\n") {
		t.Fatalf("stdout = %q, want literal target echo", stdout)
	}
	listing, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listing, "[planner]\n  tool = cursor\n") {
		t.Fatalf("planner.tool should survive: %q", listing)
	}
	if !strings.Contains(listing, "[worker]\n  model = sonnet\n") {
		t.Fatalf("worker.model should survive: %q", listing)
	}
}

// TestSettingsReset_ParseFailFast pins that a malformed `.key`
// argument aborts the batch BEFORE any store mutation.
//
// Replaces testcases/settings-reset-parse-fail-fast.md.
func TestSettingsReset_ParseFailFast(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "worker.model=sonnet", "planner.tool=cursor",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	_, _, err := testutil.RunCobra(t, settings.New(),
		"reset", "worker.model", ".key",
	)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), `bucket name must be non-empty in ".key"`) {
		t.Fatalf("err = %v, want bucket-non-empty diagnostic", err)
	}
	listing, _, lerr := testutil.RunCobra(t, settings.New())
	if lerr != nil {
		t.Fatalf("list: %v", lerr)
	}
	if !strings.Contains(listing, "model = sonnet") || !strings.Contains(listing, "tool = cursor") {
		t.Fatalf("valid targets must not be applied before the parse error: %q", listing)
	}
}

// TestSettingsReset_RepickPath pins the user flow that lets the
// agent picker re-fire on the next `j tasks start`: clearing both
// planner.tool and planner.model leaves [planner] empty so the
// next planner-bound preflight prompts again.
//
// Replaces testcases/settings-reset-rep-pick-path.md. (The actual
// agentpick.Pick branch is exercised by the picker package's own
// tests; here we just pin the persisted state.)
func TestSettingsReset_RepickPath(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "planner.tool=cursor", "planner.model=sonnet-4",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	listing, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("first list: %v", err)
	}
	if !strings.Contains(listing, "[planner]\n  model = sonnet-4\n  tool = cursor\n") {
		t.Fatalf("setup state wrong: %q", listing)
	}
	if _, _, err := testutil.RunCobra(t, settings.New(), "reset", "planner.tool"); err != nil {
		t.Fatalf("reset tool: %v", err)
	}
	listing, _, err = testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("second list: %v", err)
	}
	if !strings.Contains(listing, "[planner]\n  model = sonnet-4\n") || strings.Contains(listing, "tool = cursor") {
		t.Fatalf("intermediate state wrong: %q", listing)
	}
	if _, _, err := testutil.RunCobra(t, settings.New(), "reset", "planner.model"); err != nil {
		t.Fatalf("reset model: %v", err)
	}
	listing, _, err = testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("final list: %v", err)
	}
	if strings.Contains(listing, "tool = cursor") || strings.Contains(listing, "model = sonnet-4") {
		t.Fatalf("planner should be empty: %q", listing)
	}
}
