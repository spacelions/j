package lifecycle

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store/tasks"
)

// InitLinearPush registers the hook that mirrors planner artefacts
// (`requirements.md`, `plan.md`) back to the upstream Linear issue
// after a successful plan transition. Mirrors the shape of Init —
// both hook concerns stay independently testable.
func InitLinearPush() {
	tasks.Register(linearPushHook)
}

// isPlanSuccessEvent reports whether tr.Event is one of the four
// success-shaped events for the plan phase. Matches the events
// listed in fsm.go: foreground/reaper × done/await-approval.
func isPlanSuccessEvent(e tasks.Event) bool {
	switch e {
	case tasks.EventPlanDone,
		tasks.EventPlanAwaitApproval,
		tasks.EventReaperPlanDone,
		tasks.EventReaperPlanAwaitApproval:
		return true
	default:
		return false
	}
}

// linearPushHook reads `requirements.md` / `plan.md` from the per-
// task directory and pushes them back to the source Linear issue:
// description ← requirements.md, plus a new comment carrying
// plan.md. All failures emit a DangerousDialogBox warning to
// stderr and return — the hook never returns an error and never
// blocks the FSM transition. A failure of issueUpdate does not
// prevent commentCreate from being attempted; the two are
// independent calls.
//
// Defence-in-depth: the hook also guards on `tr.To` so any future
// edge whose Event matches `isPlanSuccessEvent` but lands outside
// `plan-done` / `plan-pending-approval` (e.g.
// `EventPlanNeedsClarification`) cannot trigger a `plan.md` upload
// against a directory that does not have one.
func linearPushHook(tr tasks.Transition, task tasks.Task) {
	if task.LinearIssue == "" {
		return
	}
	if !isPlanSuccessEvent(tr.Event) {
		return
	}
	if tr.To != tasks.StatusPlanDone &&
		tr.To != tasks.StatusPlanPendingApproval {
		return
	}
	requirements, plan, ok := readPlanArtefacts(task.ID)
	if !ok {
		return
	}
	runLinearHook(task, warnLinear, func(ctx context.Context, run linearHookRun) {
		if err := run.client.UpdateIssueDescription(
			ctx, run.issue.ID, requirements); err != nil {
			warnLinear("issueUpdate: %s", err)
		}
		if err := run.client.CreateComment(
			ctx, run.issue.ID, plan); err != nil {
			warnLinear("commentCreate: %s", err)
		}
	})
}

// readPlanArtefacts reads requirements.md and plan.md from the per-
// task directory. Either read error → warn and return ok=false so
// the hook short-circuits before any HTTP traffic. Empty contents
// (zero-byte files) round-trip as-is — Linear accepts an empty
// description / comment body and the upstream issue is no worse off.
func readPlanArtefacts(id string) (req, plan string, ok bool) {
	dir := tasks.DefaultDir()
	taskDir := filepath.Join(dir, id)
	reqBytes, err := os.ReadFile(
		filepath.Join(taskDir, tasks.RequirementsFileName))
	if err != nil {
		warnLinear("read requirements.md: %s", err)
		return "", "", false
	}
	planBytes, err := os.ReadFile(
		filepath.Join(taskDir, tasks.PlanFileName))
	if err != nil {
		warnLinear("read plan.md: %s", err)
		return "", "", false
	}
	return string(reqBytes), string(planBytes), true
}

// warnLinear emits a single orange dialog box to stderr. Background
// `j plan` already redirects stderr to the per-task agent.log;
// foreground sees it on the terminal. No extra wiring needed.
func warnLinear(format string, a ...any) {
	uitheme.DangerousDialogBox(
		os.Stderr, "linear push: "+format, a...)
}
