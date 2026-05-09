package testcases_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spacelions/j/internal/tools/linear"
)

// TestVerifier_RemindOnIssue_ReminderAtStrictlyAfterNow pins the
// core bug-fix acceptance criterion: the `reminderAt` timestamp
// sent to Linear must be strictly greater than the client's
// wall-clock time at the moment the request is observed by the
// server, so Linear's `Snooze date must be in the future` check
// (`reminderAt > server-now`) cannot trip even after RFC3339
// second-truncation, request transit, and modest clock skew.
func TestVerifier_RemindOnIssue_ReminderAtStrictlyAfterNow(
	t *testing.T,
) {
	var seen []byte
	var serverNow time.Time
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
			serverNow = time.Now().UTC()
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
	if err := c.RemindOnIssue(
		t.Context(), "node-id-future",
	); err != nil {
		t.Fatalf("RemindOnIssue: %v", err)
	}

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
	if !got.After(serverNow) {
		t.Fatalf("reminderAt %v not strictly after observed "+
			"server-now %v (raw=%q) — Linear would reject as "+
			"`Snooze date must be in the future`",
			got, serverNow, raw)
	}
}
