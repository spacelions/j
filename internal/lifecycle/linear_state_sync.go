package lifecycle

import (
	"context"
	"os"
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
// Review").
type stateSyncTarget struct {
	stateName string
}

// stateSyncTable maps each destination TaskStatus to the Linear
// workflow state. Statuses absent from the table are no-ops — the
// hook returns before any HTTP traffic. StatusPlanning is included
// so EventPlanResume / EventPlanRestart paths mirror back to Linear's
// "Todo" column when a task re-enters planning.
var stateSyncTable = map[tasks.TaskStatus]stateSyncTarget{
	tasks.StatusPlanning:            {"Todo"},
	tasks.StatusPlanDone:            {"Todo"},
	tasks.StatusPlanPendingApproval: {"Todo"},
	tasks.StatusWorking:             {"In Progress"},
	tasks.StatusVerifying:           {"In Progress"},
	tasks.StatusCompleted:           {"In Review"},
}

// InitLinearStateSync registers the hook that mirrors J's lifecycle
// onto the upstream Linear issue's workflow state. Mirrors the
// shape of InitLinearPush so the two hook concerns stay
// independently testable.
func InitLinearStateSync() {
	tasks.Register(linearStateSyncHook)
}

// linearStateSyncHook moves the linked Linear issue into the
// workflow state that mirrors tr.To. All failures emit a
// DangerousDialogBox warning to stderr and return — the hook never
// returns an error and never blocks the FSM transition.
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

// warnLinearSync emits a single orange dialog box to stderr with the
// `linear sync:` prefix so the two hooks' warnings are
// distinguishable in agent logs.
func warnLinearSync(format string, a ...any) {
	uitheme.DangerousDialogBox(
		os.Stderr, "linear sync: "+format, a...)
}
