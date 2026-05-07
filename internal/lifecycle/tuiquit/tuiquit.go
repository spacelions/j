// Package tuiquit provides post-TUI-exit reconciler functions that
// decide which FSM event to fire based on on-disk artifacts.
// All functions are pure side-effect-free decision logic.
package tuiquit

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spacelions/j/internal/store/tasks"
)

var prURLRe = regexp.MustCompile(
	`https?://github\.com/[^/\s]+/[^/\s]+/pull/\d+`)

// DecidePlan returns the event to fire based on whether plan.md
// exists and whether approval is required.
//   - plan.md present + approval → EventPlanAwaitApproval
//   - plan.md present + no approval → EventPlanDone
//   - plan.md absent → EventPlanQuit
func DecidePlan(taskDir string, requireApproval bool) (tasks.Event, error) {
	planPath := filepath.Join(taskDir, tasks.PlanFileName)
	info, err := os.Stat(planPath)
	if err != nil {
		if os.IsNotExist(err) {
			return tasks.EventPlanQuit, nil
		}
		return "", err
	}
	if info.IsDir() || info.Size() == 0 {
		return tasks.EventPlanQuit, nil
	}
	if requireApproval {
		return tasks.EventPlanAwaitApproval, nil
	}
	return tasks.EventPlanDone, nil
}

// DecideWork returns the event and (if found) PR URL based on
// whether a pull-request URL is detectable. Two-pass detection:
//  1. grep agentLogPath for a GitHub PR URL
//  2. shell out `gh pr list --head <branch>`
//
// If either pass finds a URL → EventWorkDone + url.
// If neither → EventWorkQuit.
func DecideWork(
	ctx context.Context, taskDir, branch, agentLogPath string,
) (tasks.Event, string) {
	if url := prURLInAgentLog(agentLogPath); url != "" {
		return tasks.EventWorkDone, url
	}
	if url := runGhPRList(ctx, branch); url != "" {
		return tasks.EventWorkDone, url
	}
	return tasks.EventWorkQuit, ""
}

// DecideVerify returns the event based on the last non-empty line of
// verifier_findings.md.
//   - VERDICT: PASS → EventVerifyPass
//   - VERDICT: FAIL → EventVerifyFail
//   - missing / other → EventVerifyQuit
func DecideVerify(taskDir string) tasks.Event {
	path := filepath.Join(taskDir, tasks.VerifierFindingsFileName)
	f, err := os.Open(path)
	if err != nil {
		return tasks.EventVerifyQuit
	}
	defer f.Close()
	last := lastNonEmptyLine(f)
	switch {
	case strings.Contains(last, "VERDICT: PASS"):
		return tasks.EventVerifyPass
	case strings.Contains(last, "VERDICT: FAIL"):
		return tasks.EventVerifyFail
	}
	return tasks.EventVerifyQuit
}

func lastNonEmptyLine(r io.Reader) string {
	var last string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			last = line
		}
	}
	return last
}

func prURLInAgentLog(agentLogPath string) string {
	f, err := os.Open(agentLogPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	return parseAgentLogForPR(f)
}

// parseAgentLogForPR scans r for the first GitHub PR URL match.
func parseAgentLogForPR(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		m := prURLRe.FindString(sc.Text())
		if m != "" {
			return m
		}
	}
	return ""
}

// ghTimeout bounds the `gh pr list` shell-out.
const ghTimeout = 5 * time.Second

// runGhPRList shells out `gh pr list --head <branch> --json url
// --jq '.[0].url'` with a timeout. Returns empty string on any
// failure.
func runGhPRList(ctx context.Context, branch string) string {
	if branch == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--head", branch,
		"--json", "url",
		"--jq", ".[0].url",
	)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
