package lifecycle

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store/tasks"
)

// linearVerifyPushTimeout bounds the total time the verify-push hook
// spends talking to Linear. Mirrors linearPushTimeout so the family
// of hooks shares an identical worst-case budget.
const linearVerifyPushTimeout = 30 * time.Second

// verifyTerminalHeaders maps each terminal verify event onto the
// human-readable header that should accompany the verifier_findings.md
// body when it lands on the Linear issue. Events outside this map are
// no-ops — the hook returns before any HTTP traffic.
var verifyTerminalHeaders = map[tasks.Event]string{
	tasks.EventVerifyPass:  "Verification passed",
	tasks.EventVerifyFail:  "Verification failed (retries exhausted)",
	tasks.EventVerifyStuck: "Verification failed (retries exhausted)",
}

// InitLinearVerifyPush registers the hook that mirrors the verifier
// findings file to the linked Linear issue on every terminal verify
// transition. Mirrors the shape of InitLinearPush /
// InitLinearStateSync so the three hook concerns stay independently
// testable.
func InitLinearVerifyPush() {
	tasks.Register(linearVerifyPushHook)
}

// linearVerifyPushHook posts a `<header>\n\n<findings>` plain
// comment to the linked Linear issue when the verifier transitions
// to a terminal state. The comment is self-authored, so Linear's
// actor==recipient gate suppresses any inbox notification — exactly
// what we want here (the comment is for context on the issue, not a
// page). All failures emit a stderr warning and return.
func linearVerifyPushHook(tr tasks.Transition, task tasks.Task) {
	header, ok := verifyTerminalHeaders[tr.Event]
	if !ok {
		return
	}
	pushFindings(os.Stderr, task, header)
}

// PushVerifyIterationFinding posts a per-iteration plain comment to
// the linked Linear issue. Called by the verifier loop after each
// FAIL verdict; iteration is 0-based and is rendered as 1-based in
// the comment header for human readability.
func PushVerifyIterationFinding(
	stderr io.Writer, task tasks.Task, iteration, iterMax int,
) {
	header := fmt.Sprintf(
		"Verification iteration %d/%d failed", iteration+1, iterMax,
	)
	pushFindings(stderr, task, header)
}

// pushFindings is the shared worker that reads verifier_findings.md
// for the task and posts a plain comment carrying the supplied
// header plus the file body. Each step warns and returns on failure;
// the caller's stderr is honoured so the verifier loop's redirection
// (agent.log via background runner) keeps working.
func pushFindings(stderr io.Writer, task tasks.Task, header string) {
	if task.LinearIssue == "" {
		return
	}
	findings, ok := readFindings(stderr, task.ID)
	if !ok {
		return
	}
	token, ok := loadVerifyToken(stderr)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(
		context.Background(), linearVerifyPushTimeout)
	defer cancel()
	client := linear.NewClient(token)
	issue, err := client.GetIssue(ctx, task.LinearIssue)
	if err != nil {
		warnLinearVerify(
			stderr, "resolve %s: %s", task.LinearIssue, err)
		return
	}
	body := header + "\n\n" + findings
	if err := client.CreateComment(
		ctx, issue.ID, body); err != nil {
		warnLinearVerify(stderr, "commentCreate: %s", err)
	}
}

// readFindings loads `<tasksDir>/<id>/verifier_findings.md`. Either
// step warning short-circuits the hook before any HTTP traffic.
func readFindings(stderr io.Writer, id string) (string, bool) {
	dir, err := tasks.DefaultDir()
	if err != nil {
		warnLinearVerify(stderr, "tasks dir: %s", err)
		return "", false
	}
	path := filepath.Join(dir, id, tasks.VerifierFindingsFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		warnLinearVerify(
			stderr, "read %s: %s", tasks.VerifierFindingsFileName, err)
		return "", false
	}
	return string(data), true
}

// loadVerifyToken returns the Linear API key and ok=true on success,
// or warns and returns ok=false when the key is missing / unreadable.
// Mirrors loadLinearToken from the state-sync hook with this hook's
// warn prefix.
func loadVerifyToken(stderr io.Writer) (string, bool) {
	token, err := linear.LoadAPIKey()
	if err != nil {
		warnLinearVerify(stderr, "load api key: %s", err)
		return "", false
	}
	if token == "" {
		warnLinearVerify(stderr, "no API key set")
		return "", false
	}
	return token, true
}

// warnLinearVerify emits a single orange dialog box with the
// `linear verify push:` prefix so the three hooks' warnings are
// distinguishable in agent logs.
func warnLinearVerify(stderr io.Writer, format string, a ...any) {
	uitheme.DangerousDialogBox(
		stderr, "linear verify push: "+format, a...)
}
