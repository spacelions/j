package testcases_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/linear"
)

// TestVerifier_RemindOnIssue_ReminderAtIsRFC3339UTC pins the
// formatting half of the acceptance criterion: the wire value of
// `reminderAt` is a valid RFC3339 timestamp in UTC (suffix "Z"),
// not a local-zone offset, and not RFC3339Nano (no fractional
// seconds), since fractional seconds are exactly what the spec
// truncates and what the bug originally exposed.
func TestVerifier_RemindOnIssue_ReminderAtIsRFC3339UTC(
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
	if err := c.RemindOnIssue(
		context.Background(), "node-id-fmt",
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
	if !strings.HasSuffix(raw, "Z") {
		t.Fatalf("reminderAt %q is not UTC (no `Z` suffix); "+
			"requirement specifies UTC RFC3339", raw)
	}
	if strings.Contains(raw, ".") {
		t.Fatalf("reminderAt %q has fractional seconds; "+
			"requirement specifies RFC3339 (no sub-second "+
			"precision)", raw)
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("reminderAt parse via RFC3339: %v (raw=%q)",
			err, raw)
	}
	if parsed.Location() != time.UTC {
		t.Fatalf("reminderAt parsed location %v, want UTC "+
			"(raw=%q)", parsed.Location(), raw)
	}
}
