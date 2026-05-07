package testcases_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_RemindAtIsRFC3339Now pins the "remindAt ≈
// now (RFC3339, UTC)" acceptance criterion. The reminder must
// surface immediately in Linear's inbox, which Linear interprets
// from the remindAt timestamp; sending an RFC3339 timestamp
// snapped to "now" is what guarantees the inbox ping fires
// without a delay window.
func TestLinearStateSync_RemindAtIsRFC3339Now(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	before := time.Now().UTC().Add(-2 * time.Second)
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	after := time.Now().UTC().Add(2 * time.Second)

	got := env.recordedBodies()
	if len(got) < 4 {
		t.Fatalf("expected ≥4 bodies, got %d: %v", len(got), got)
	}
	last := got[3]
	if !strings.Contains(last, "issueRemindMe") {
		t.Fatalf("4th body = %q, want issueRemindMe", last)
	}
	var req struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(last), &req); err != nil {
		t.Fatalf("decode body: %v (%s)", err, last)
	}
	raw, ok := req.Variables["remindAt"].(string)
	if !ok {
		t.Fatalf("remindAt = %v (%T)",
			req.Variables["remindAt"],
			req.Variables["remindAt"])
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("remindAt parse: %v (raw=%q)", err, raw)
	}
	if parsed.Before(before) || parsed.After(after) {
		t.Fatalf("remindAt = %v, want within [%v, %v]",
			parsed, before, after)
	}
}
