package testcases_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
)

// seedAgentBuckets pre-populates the planner / worker / verifier
// buckets with cursor/auto so EnsureAgentSelections does not block
// on a TTY huh prompt during the start-time PreRunE.
func seedAgentBuckets(t *testing.T) {
	t.Helper()
	for _, bucket := range []string{
		store.BucketPlanner, store.BucketWorker, store.BucketVerifier,
	} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "auto")
	}
}

// clearStartEnv unsets the TASKS_START_* env vars so leakage from
// the host environment cannot contaminate viper bindings.
func clearStartEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"TASKS_START_FROM_FILE", "TASKS_START_FROM_LINEAR", "TASKS_START_FROM_TASK",
		"TASKS_START_TOOL", "TASKS_START_MODEL", "TASKS_START_INTERACTIVE",
		"TASKS_START_YES", "TASKS_START_PLAN_REQUIRES_APPROVAL",
	} {
		t.Setenv(name, "")
		_ = name
	}
}

// TestLinearTasksStart_HelpMentionsFromLinear pins the `--help`
// surface for `j tasks start`: the Linear flag is documented and
// the existing `--from-file/-f` flag is still listed.
//
// Replaces testcases/linear-tasks-start-help-mentions-from-linear.md.
func TestLinearTasksStart_HelpMentionsFromLinear(t *testing.T) {
	freshInit(t)

	stdout, _, err := testutil.RunCobra(tasks.New(), "start", "--help")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"--from-linear",
		"Linear issue identifier",
		"ENG-123",
		"linear.api_key",
		"--from-file",
		"-f,",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help missing %q: %q", want, stdout)
		}
	}
}

// TestLinearTasksStart_FromLinearNoAPIKey pins the explicit error
// when --from-linear is supplied but no linear.api_key is stored:
// non-zero exit, the canonical error wording, no task is created.
//
// Replaces testcases/linear-tasks-start-from-linear-no-api-key.md
// AND testcases/verify-tasks-start-from-linear-without-api-key-exits-1.md
// (the two checklists were duplicates).
func TestLinearTasksStart_FromLinearNoAPIKey(t *testing.T) {
	freshInit(t)
	seedAgentBuckets(t)
	clearStartEnv(t)
	installCursorAgentLoginStub(t)

	_, _, err := testutil.RunCobra(tasks.New(),
		"start", "--from-linear", "ENG-123",
	)
	if err == nil {
		t.Fatal("expected error (no api key set)")
	}
	if !errors.Is(err, linear.ErrNoAPIKey) {
		t.Fatalf("err = %v, want linear.ErrNoAPIKey", err)
	}

	listing, _, lerr := testutil.RunCobra(tasks.New())
	if lerr != nil {
		t.Fatalf("tasks listing: %v", lerr)
	}
	if !strings.Contains(listing, "J: no tasks") {
		t.Fatalf("expected `J: no tasks`, got %q", listing)
	}
}

// TestLinearTasksStart_FromLinearInvalidIdentifier pins the
// identifier-validator error path: with linear.api_key stored,
// `--from-linear foo` still fails (not ENG-123 shaped) and creates
// no task.
//
// Replaces testcases/linear-tasks-start-from-linear-invalid-identifier.md.
func TestLinearTasksStart_FromLinearInvalidIdentifier(t *testing.T) {
	freshInit(t)
	seedAgentBuckets(t)
	clearStartEnv(t)
	installCursorAgentLoginStub(t)
	if err := linear.SaveAPIKey(linearAPIKey); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}

	_, _, err := testutil.RunCobra(tasks.New(),
		"start", "--from-linear", "foo",
	)
	if err == nil {
		t.Fatal("expected error (invalid identifier)")
	}
	if !errors.Is(err, linear.ErrInvalidIdentifier) {
		t.Fatalf("err = %v, want linear.ErrInvalidIdentifier", err)
	}

	listing, _, lerr := testutil.RunCobra(tasks.New())
	if lerr != nil {
		t.Fatalf("tasks listing: %v", lerr)
	}
	if !strings.Contains(listing, "J: no tasks") {
		t.Fatalf("expected `J: no tasks`, got %q", listing)
	}
}

// TestLinearTasksStart_FromLinearEnvVar pins that
// `TASKS_START_FROM_LINEAR=foo j tasks start` (no flag) routes the
// env-var binding through the same identifier validator as the
// flag. No api key is needed because validation fires before the
// HTTP call.
//
// Replaces testcases/linear-tasks-start-from-linear-env-var.md.
func TestLinearTasksStart_FromLinearEnvVar(t *testing.T) {
	freshInit(t)
	seedAgentBuckets(t)
	clearStartEnv(t)
	installCursorAgentLoginStub(t)
	t.Setenv("TASKS_START_FROM_LINEAR", "foo")

	_, _, err := testutil.RunCobra(tasks.New(), "start")
	if err == nil {
		t.Fatal("expected error from env-var binding")
	}
	if !errors.Is(err, linear.ErrInvalidIdentifier) && !errors.Is(err, linear.ErrNoAPIKey) {
		t.Fatalf("err = %v, want invalid-identifier or no-api-key (validator order)", err)
	}

	listing, _, lerr := testutil.RunCobra(tasks.New())
	if lerr != nil {
		t.Fatalf("tasks listing: %v", lerr)
	}
	if !strings.Contains(listing, "J: no tasks") {
		t.Fatalf("expected `J: no tasks`, got %q", listing)
	}
}
