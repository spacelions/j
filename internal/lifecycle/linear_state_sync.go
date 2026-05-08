package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store/tasks"
)

// linearStateSyncTimeout bounds the total time the hook spends
// talking to Linear. Mirrors linearPushTimeout so the two hooks
// share an identical worst-case budget.
const linearStateSyncTimeout = 30 * time.Second

// stateSyncTarget describes how a destination TaskStatus should be
// mirrored into Linear: stateName is the human-readable workflow
// state to switch the issue to ("Todo", "In Progress", "In
// Review"); ping=true also schedules a Linear inbox reminder for
// the API-key owner so they are surfaced when human attention is
// required.
type stateSyncTarget struct {
	stateName string
	ping      bool
}

// stateSyncTable maps each destination TaskStatus to the Linear
// workflow state and follow-up reminder behaviour. Statuses absent
// from the table are no-ops — the hook returns before any HTTP
// traffic. `Planning` is mapped (ping=false) so re-plan and
// resume-plan transitions roll the upstream Linear issue back to
// `Todo` without paging the owner; the user initiated the rollback.
// `NeedsClarification` is mirrored to "In Progress" with ping=false
// because its dedicated branch in linearStateSyncHook posts the
// clarification body as a comment and schedules the inbox reminder
// itself — mirroring the verify-begin branch.
var stateSyncTable = map[tasks.TaskStatus]stateSyncTarget{
	tasks.StatusPlanning:            {"Todo", false},
	tasks.StatusPlanDone:            {"Todo", true},
	tasks.StatusPlanPendingApproval: {"Todo", true},
	tasks.StatusWorking:             {"In Progress", false},
	tasks.StatusVerifying:           {"In Progress", false},
	tasks.StatusNeedsClarification:  {"In Progress", false},
	tasks.StatusCompleted:           {"In Review", true},
}

// InitLinearStateSync registers the hook that mirrors J's lifecycle
// onto the upstream Linear issue's workflow state. Mirrors the
// shape of InitLinearPush so the two hook concerns stay
// independently testable.
func InitLinearStateSync() {
	tasks.Register(linearStateSyncHook)
}

// linearStateSyncHook moves the linked Linear issue into the
// workflow state that mirrors tr.To, and optionally schedules a
// Linear inbox reminder for the API-key owner when the destination
// warrants human attention. The verify-begin transition additionally
// posts a comment carrying the PR URL and pings the owner. All
// failures emit a DangerousDialogBox warning to stderr and return —
// the hook never returns an error and never blocks the FSM
// transition. Failures of issueUpdate do not prevent the follow-up
// comment / reminder from being attempted.
func linearStateSyncHook(tr tasks.Transition, task tasks.Task) {
	if task.LinearIssue == "" {
		return
	}
	target, ok := stateSyncTable[tr.To]
	if !ok {
		return
	}
	token, ok := loadLinearToken()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(
		context.Background(), linearStateSyncTimeout)
	defer cancel()
	client := linear.NewClient(token)
	issue, err := client.GetIssue(ctx, task.LinearIssue)
	if err != nil {
		warnLinearSync("resolve %s: %s", task.LinearIssue, err)
		return
	}
	stateID, ok := resolveStateID(ctx, client, issue.ID, target.stateName)
	if !ok {
		return
	}
	if err := client.UpdateIssueState(
		ctx, issue.ID, stateID); err != nil {
		warnLinearSync("issueUpdate: %s", err)
	}
	if tr.To == tasks.StatusNeedsClarification &&
		isNeedsClarificationEvent(tr.Event) {
		handleNeedsClarification(ctx, client, issue.ID, task)
		return
	}
	if tr.To == tasks.StatusVerifying &&
		tr.Event == tasks.EventVerifyBegin &&
		task.PullRequestURL != "" {
		postPullRequestComment(ctx, client, issue.ID, task.PullRequestURL)
		postInboxReminder(ctx, client, issue.ID)
		return
	}
	if target.ping {
		postInboxReminder(ctx, client, issue.ID)
	}
}

// isNeedsClarificationEvent narrows the comment+reminder branch to
// the four entries into `needs-clarification`: the foreground planner
// exit (`EventPlanNeedsClarification`) plus the three reaper-driven
// events. Resume events leaving the state, or any unrelated
// transition, must NOT trigger the comment / reminder traffic.
func isNeedsClarificationEvent(ev tasks.Event) bool {
	switch ev {
	case tasks.EventPlanNeedsClarification,
		tasks.EventReaperPlanNeedsClarification,
		tasks.EventReaperWorkNeedsClarification,
		tasks.EventReaperVerifyNeedsClarification:
		return true
	}
	return false
}

// handleNeedsClarification posts the clarification.md body as a
// Linear comment and schedules an inbox reminder. The two follow-up
// calls are independent best-effort steps: a missing/empty/unreadable
// clarification.md — including a missing AgentLogPath — skips the
// comment but still sends the reminder, so the human is paged either
// way.
func handleNeedsClarification(
	ctx context.Context, client *linear.Client,
	issueID string, task tasks.Task,
) {
	if task.AgentLogPath == "" {
		warnLinearSync("clarification: no agent log path")
	} else {
		taskDir := filepath.Dir(task.AgentLogPath)
		postClarificationComment(ctx, client, issueID, taskDir)
	}
	postInboxReminder(ctx, client, issueID)
}

// postClarificationComment reads <taskDir>/clarification.md and posts
// its content as a Linear comment. Warns and returns on read error or
// empty body so the caller can still emit the inbox reminder. Warns
// (but does not return) on commentCreate failure for the same reason.
func postClarificationComment(
	ctx context.Context, client *linear.Client,
	issueID, taskDir string,
) {
	path := filepath.Join(taskDir, "clarification.md")
	body, err := os.ReadFile(path)
	if err != nil {
		warnLinearSync("read clarification.md: %s", err)
		return
	}
	if strings.TrimSpace(string(body)) == "" {
		warnLinearSync("clarification.md is empty")
		return
	}
	if err := client.CreateComment(
		ctx, issueID, string(body)); err != nil {
		warnLinearSync("commentCreate: %s", err)
	}
}

// loadLinearToken returns the Linear API key and ok=true on success,
// or warns and returns ok=false when the key is missing / unreadable
// — mirroring the linear-push hook's preflight.
func loadLinearToken() (string, bool) {
	token, err := linear.LoadAPIKey()
	if err != nil {
		warnLinearSync("load api key: %s", err)
		return "", false
	}
	if token == "" {
		warnLinearSync("no API key set")
		return "", false
	}
	return token, true
}

// resolveStateID asks Linear for the workflow states attached to the
// issue's team, picks the one whose Name matches stateName, and
// returns its node id. Warns and returns ok=false on transport error
// or when the state is absent from the team.
func resolveStateID(
	ctx context.Context, client *linear.Client,
	issueID, stateName string,
) (string, bool) {
	states, err := client.ListTeamWorkflowStates(ctx, issueID)
	if err != nil {
		warnLinearSync("list states: %s", err)
		return "", false
	}
	state, ok := linear.FindStateByName(states, stateName)
	if !ok {
		warnLinearSync("workflow state %q not found", stateName)
		return "", false
	}
	return state.ID, true
}

// postInboxReminder schedules a Linear inbox reminder on the issue
// for the API-key owner. Linear surfaces the reminder effectively
// immediately; RemindOnIssue passes a near-future reminderAt
// timestamp because Linear rejects `reminderAt <= now`.
// Warns on error and never blocks — failures here must not change
// the J task status.
func postInboxReminder(
	ctx context.Context, client *linear.Client, issueID string,
) {
	if err := client.RemindOnIssue(ctx, issueID); err != nil {
		warnLinearSync("issueReminder: %s", err)
	}
}

// postPullRequestComment posts the GitHub PR URL as a plain comment
// on the linked Linear issue so click-through from the inbox
// reminder lands on the PR. Warns on error and never blocks.
func postPullRequestComment(
	ctx context.Context, client *linear.Client, issueID, prURL string,
) {
	if err := client.CreateComment(
		ctx, issueID, prURL); err != nil {
		warnLinearSync("commentCreate: %s", err)
	}
}

// warnLinearSync emits a single orange dialog box to stderr with the
// `linear sync:` prefix so the two hooks' warnings are
// distinguishable in agent logs.
func warnLinearSync(format string, a ...any) {
	uitheme.DangerousDialogBox(
		os.Stderr, "linear sync: "+format, a...)
}
