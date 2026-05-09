package lifecycle

import (
	"context"
	"strings"
	"unicode"

	"github.com/spacelions/j/internal/store/tasks"
)

// alertPrefix decorates titles whose task entered an
// "abnormal / needs attention" status (needs-clarification, failed,
// help). The trailing space separates the emoji from the original
// title text.
const alertPrefix = "❗ "

// eyesPrefix decorates titles whose task is parked on
// `plan-pending-approval`, signalling that the human needs to look at
// the plan before work begins.
const eyesPrefix = "👀 "

// InitLinearTitleSync registers the hook that mirrors the J task
// status onto the linked Linear issue's title via an emoji prefix.
// Sibling to InitLinearStateSync; the two hooks are independent
// observers of the same transition stream.
func InitLinearTitleSync() {
	tasks.Register(linearTitleSyncHook)
}

// linearTitleSyncHook decorates the linked Linear issue's title to
// reflect tr.To. Tasks without a Linear link, or environments
// without a Linear API key, are silent no-ops. All failures route
// through warnLinearSync so they appear in the same agent-log channel
// as other Linear sync warnings; the hook never blocks the FSM.
func linearTitleSyncHook(tr tasks.Transition, task tasks.Task) {
	if task.LinearIssue == "" {
		return
	}
	runLinearHook(task, warnLinearSync, func(
		ctx context.Context, run linearHookRun,
	) {
		newTitle := decorateTitle(run.issue.Title, tr.To)
		if newTitle == run.issue.Title {
			return
		}
		if err := run.client.UpdateIssueTitle(
			ctx, run.issue.ID, newTitle); err != nil {
			warnLinearSync("issueUpdate title: %s", err)
		}
	})
}

// decorateTitle returns current with any existing ❗/👀 prefix
// stripped and the prefix dictated by status reapplied. Pure helper
// — no I/O — so it carries the bulk of the unit-test coverage.
func decorateTitle(current string, status tasks.TaskStatus) string {
	return prefixFor(status) + stripStatusPrefix(current)
}

// stripStatusPrefix removes any leading ❗/👀 runes (and the spaces
// that surround them) from s, looping so accumulated or duplicated
// prefixes collapse to a single clean baseline. Whitespace strictly
// inside the original title is preserved — the loop only trims
// whitespace adjacent to a prefix rune it is removing.
func stripStatusPrefix(s string) string {
	for {
		trimmed := strings.TrimLeftFunc(s, unicode.IsSpace)
		next, ok := trimOnePrefix(trimmed)
		if !ok {
			return s
		}
		s = next
	}
}

// trimOnePrefix removes a single leading ❗ or 👀 rune (with the
// space that typically follows in our decoration). Returns the
// remainder and ok=true if a prefix was found, otherwise the input
// untouched and ok=false. Splitting this out keeps stripStatusPrefix
// inside the 80-line method cap and makes the loop guard trivially
// readable.
func trimOnePrefix(s string) (string, bool) {
	switch {
	case strings.HasPrefix(s, alertPrefix):
		return s[len(alertPrefix):], true
	case strings.HasPrefix(s, eyesPrefix):
		return s[len(eyesPrefix):], true
	case strings.HasPrefix(s, "❗"):
		return s[len("❗"):], true
	case strings.HasPrefix(s, "👀"):
		return s[len("👀"):], true
	}
	return s, false
}

// prefixFor maps a destination TaskStatus to the leading decoration
// the title should carry. Statuses outside the abnormal /
// approval-pending set return "" so any prior decoration is stripped
// without re-applying one.
func prefixFor(status tasks.TaskStatus) string {
	switch status {
	case tasks.StatusNeedsClarification,
		tasks.StatusFailed,
		tasks.StatusHelp:
		return alertPrefix
	case tasks.StatusPlanPendingApproval:
		return eyesPrefix
	default:
		return ""
	}
}
