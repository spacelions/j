package testcases_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spacelions/j/internal/linear"
)

// TestVerifier_RemindOnIssue_ReminderAtIsAboutOneMinuteAhead
// pins the chosen offset magnitude from the requirement: the
// reminderAt timestamp is bumped roughly +1 minute relative to
// `time.Now()`. The window is intentionally generous (≥30s,
// ≤2m) so it is robust to scheduler jitter on slow CI yet still
// rejects the pre-fix behavior (offset ≈0) and any future drift
// to a much larger snooze (e.g. hours).
func TestVerifier_RemindOnIssue_ReminderAtIsAboutOneMinuteAhead(
	t *testing.T,
) {
	var seen []byte
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"issueReminder": map[string]any{
						"success": true,
					},
				},
			})
		}))
	t.Cleanup(srv.Close)

	c := linear.NewClient("k", linear.WithEndpoint(srv.URL))
	callBefore := time.Now().UTC()
	if err := c.RemindOnIssue(
		t.Context(), "node-id-offset",
	); err != nil {
		t.Fatalf("RemindOnIssue: %v", err)
	}
	callAfter := time.Now().UTC()

	var req struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(seen, &req); err != nil {
		t.Fatalf("decode body: %v (%s)", err, seen)
	}
	raw, ok := req.Variables["reminderAt"].(string)
	if !ok {
		t.Fatalf("reminderAt var = %v (%T)",
			req.Variables["reminderAt"],
			req.Variables["reminderAt"])
	}
	got, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("reminderAt parse: %v (raw=%q)", err, raw)
	}

	// Lower bound: reminderAt must be at least ~30s ahead of the
	// earliest plausible "now" the client sampled. RFC3339 truncates
	// sub-second precision so allow up to 1s slop.
	minOffset := 30 * time.Second
	if got.Before(callBefore.Add(minOffset - time.Second)) {
		t.Fatalf("reminderAt %v is less than %v ahead of "+
			"call-before %v (raw=%q) — would not clear "+
			"truncation+transit safely",
			got, minOffset, callBefore, raw)
	}
	// Upper bound: must not be a long snooze; ≤2 minutes ahead of
	// the latest plausible "now" the client sampled.
	maxOffset := 2 * time.Minute
	if got.After(callAfter.Add(maxOffset)) {
		t.Fatalf("reminderAt %v is more than %v ahead of "+
			"call-after %v (raw=%q) — offset is no longer "+
			"the immediate ping the requirement specifies",
			got, maxOffset, callAfter, raw)
	}
}
