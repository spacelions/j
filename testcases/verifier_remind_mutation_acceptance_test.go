package testcases_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/linear"
)

// TestVerifier_RemindOnIssue_UsesIssueReminderMutation pins the
// acceptance criterion that the GraphQL outbound body uses the new
// field name `issueReminder` and the new variable key `reminderAt`,
// and contains no trace of the rejected `issueRemindMe`/`remindAt`
// tokens. It is intentionally black-box: it exercises only the
// public linear.Client and inspects the wire body the server saw.
func TestVerifier_RemindOnIssue_UsesIssueReminderMutation(
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
	before := time.Now().UTC().Add(-2 * time.Second)
	if err := c.RemindOnIssue(
		t.Context(), "node-id-99",
	); err != nil {
		t.Fatalf("RemindOnIssue: %v", err)
	}
	after := time.Now().UTC().Add(time.Minute + 2*time.Second)

	body := string(seen)
	if !strings.Contains(body, "issueReminder(") {
		t.Fatalf("body missing issueReminder(: %s", body)
	}
	if strings.Contains(body, "issueRemindMe") {
		t.Fatalf("body still contains rejected "+
			"issueRemindMe token: %s", body)
	}
	if !strings.Contains(body, "$reminderAt:DateTime!") {
		t.Fatalf("body missing $reminderAt:DateTime! decl: %s",
			body)
	}
	if !strings.Contains(body, "reminderAt:$reminderAt") {
		t.Fatalf("body missing reminderAt:$reminderAt arg: %s",
			body)
	}
	if strings.Contains(body, "remindAt") {
		t.Fatalf("body still contains old remindAt token: %s",
			body)
	}

	var req struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(seen, &req); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if req.Variables["id"] != "node-id-99" {
		t.Fatalf("id var = %v, want node-id-99",
			req.Variables["id"])
	}
	if _, present := req.Variables["remindAt"]; present {
		t.Fatalf("variables still has stale remindAt key: %v",
			req.Variables)
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
	if got.Before(before) || got.After(after) {
		t.Fatalf("reminderAt = %v, want within [%v, %v]",
			got, before, after)
	}
}
