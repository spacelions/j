package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearPush_MissingAPIKey_WarnsAndContinues pins the
// "best-effort: any failure emits a DangerousDialogBox warning on
// stderr and the planner phase still reports success" acceptance
// criterion. With no Linear API key configured, the hook must:
//   - emit a warning to stderr (DangerousDialogBox shape)
//   - never reach an HTTP call
//   - return without panic so the FSM transition completes
//
// `tasks.Notify` returning normally is the proxy for "transition
// completes": the hook runs after the persisted transition and a
// nil-or-error return from a hook is intentionally ignored by the
// notify pipeline.
func TestLinearPush_MissingAPIKey_WarnsAndContinues(t *testing.T) {
	id := tasks.NewTaskID()
	env := newLinearPushEnv(t, id, "req", "plan")
	// Deliberately do NOT call saveLinearAPIKey.

	lifecycle.InitLinearPush()
	firePlanDone(id, "ENG-1", tasks.EventPlanDone)

	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected zero HTTP traffic with no api key, got %v",
			got)
	}
	msg := env.stderrText(t)
	if !strings.Contains(msg, "linear push") {
		t.Fatalf("stderr = %q, want a 'linear push' warning", msg)
	}
}
