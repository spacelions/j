package testcases_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestCase_PRURL_VerifyBegin_CarriesThroughFromWorkEnd pins the
// reported-bug acceptance criterion (#3) end-to-end through real
// production wiring. After WorkLifecycle.Finish detects the PR URL
// from agent.log, the persisted task row carries pull_request_url
// into the very next BeginVerifyRestart, which fires EventVerifyBegin
// and lights up the linearStateSyncHook PR-link branch (commentCreate
// with the URL + issueReminder). This is the exact transition that
// silently skipped on production tasks before the fix.
func TestCase_PRURL_VerifyBegin_CarriesThroughFromWorkEnd(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearStateSync()

	logPath := filepath.Join(t.TempDir(), "agent.log")
	prURL := "https://github.com/spacelions/j/pull/777"
	if err := os.WriteFile(logPath,
		[]byte("opened "+prURL+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := tasks.NewTaskID()
	seed := tasks.Task{
		ID:          id,
		Status:      tasks.StatusPlanDone,
		Summary:     "drives verify-begin",
		LinearIssue: "ENG-1",
	}
	tasks.PersistWarn(io.Discard, seed)

	work := lifecycle.BeginWorkRestart(seed, io.Discard,
		"cursor", "sonnet-4", "work-cursor", logPath)
	work.Finish(nil)

	finished := work.Task()
	if finished.PullRequestURL != prURL {
		t.Fatalf("after Finish: PullRequestURL = %q, want %q",
			finished.PullRequestURL, prURL)
	}
	if finished.LinearIssue != "ENG-1" {
		t.Fatalf("after Finish: LinearIssue = %q, want ENG-1",
			finished.LinearIssue)
	}

	_ = lifecycle.BeginVerifyRestart(finished, io.Discard,
		"cursor", "sonnet-4", "verify-cursor", logPath)

	if !verifyBeginPRLinkCalls(t, env.recordedBodies(), prURL) {
		t.Fatalf("verify-begin PR-link branch did not fire with URL")
	}
}

// verifyBeginPRLinkCalls scans the recorded Linear bodies for the
// signature of the verify-begin PR-link branch: a commentCreate whose
// body equals prURL, immediately followed by an issueReminder. Returns
// true if found.
func verifyBeginPRLinkCalls(
	t *testing.T, bodies []string, prURL string,
) bool {
	t.Helper()
	kinds := bodyKindList(bodies)
	for i := 0; i+1 < len(kinds); i++ {
		if kinds[i] != "commentCreate" {
			continue
		}
		if kinds[i+1] != "reminder" {
			continue
		}
		if decodeMutationVar(t, bodies[i], "body") != prURL {
			continue
		}
		return true
	}
	return false
}
