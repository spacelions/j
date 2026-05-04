package tasks

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/run"
)

// reapBackgroundTasks finalises rows whose fire-and-forget background
// child has exited. For every entry in `in` whose Status is one of
// the active "in flight" set (planning / working) and whose
// BackgroundPID is non-zero, the helper polls run.IsAlive and:
//
//   - on `planning` with the child gone, transitions the row to
//     `plan-done` when both `requirements.md` and `plan.md` exist
//     under `<tasksDir>/<id>/`, refreshes Summary from
//     `requirements.md`, and stamps PlanEndAt; otherwise it sets
//     Status to `help` and stamps PlanEndAt.
//   - on `working` with the child gone, transitions the row to
//     `work-done` and stamps WorkEndAt. Work has no single output
//     artifact to inspect so failures surface via the agent log
//     captured at AgentLogPath.
//
// In both cases BackgroundPID is cleared so subsequent reaper runs
// skip the row. AgentLogPath is preserved so users can still
// discover the trailing log via the bbolt row.
//
// Persistence is best-effort: PutTask errors warn on stderr and the
// reaper continues with the next entry. Rows whose child is still
// alive are returned untouched (in particular without re-persisting,
// so no superfluous bbolt writes happen when nothing changed).
//
// The function returns a refreshed slice in the same order as the
// input so the printer reflects the just-applied transitions
// without forcing a re-read of the DB.
func reapBackgroundTasks(s *store.Store, stderr io.Writer, tasksDir string, in []store.Task) []store.Task {
	out := make([]store.Task, len(in))
	for i, t := range in {
		out[i] = maybeReap(s, stderr, tasksDir, t)
	}
	return out
}

// maybeReap is the per-row helper for reapBackgroundTasks. It
// short-circuits rows without a recorded BackgroundPID and rows in
// non-reapable statuses, polls run.IsAlive otherwise, and dispatches
// to the status-specific finaliser when the child has exited. Only
// `planning` and `working` rows are reapable; `help`, `plan-done`,
// and other states are left alone so a stale PID (e.g. from a
// crashed parent) does not silently mutate finalised data.
func maybeReap(s *store.Store, stderr io.Writer, tasksDir string, t store.Task) store.Task {
	if t.BackgroundPID == 0 {
		return t
	}
	if run.IsAlive(t.BackgroundPID) {
		return t
	}
	switch t.Status {
	case store.StatusPlanning:
		t = finalisePlanReap(tasksDir, t)
	case store.StatusWorking:
		t = finaliseWorkReap(t)
	default:
		return t
	}
	if err := s.PutTask(t); err != nil {
		banner.DangerousFprintf(stderr, "J: warning: tasks put: %v\n", err)
	}
	return t
}

// finalisePlanReap promotes a `planning` row whose background child
// has exited. The transition is gated on the on-disk artifacts the
// agent was supposed to produce: when both `requirements.md` and
// `plan.md` exist under <tasksDir>/<id>/, the row flips to
// `plan-done` and Summary is refreshed from requirements.md; when
// either is missing the row flips to `help` instead so the user
// notices the failed run via `j tasks`. PlanEndAt is stamped in
// either branch and BackgroundPID is cleared.
func finalisePlanReap(tasksDir string, t store.Task) store.Task {
	taskDir := filepath.Join(tasksDir, t.ID)
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	planPath := filepath.Join(taskDir, store.PlanFileName)
	end := time.Now().UTC()
	t.PlanEndAt = &end
	t.BackgroundPID = 0
	reqData, reqErr := os.ReadFile(requirementsPath)
	_, planErr := os.Stat(planPath)
	if reqErr != nil || planErr != nil {
		t.Status = store.StatusHelp
		return t
	}
	t.Status = store.StatusPlanDone
	if summary := store.SummarizeMarkdown(string(reqData)); summary != "" {
		t.Summary = summary
	}
	return t
}

// finaliseWorkReap promotes a `working` row whose background child
// has exited to `work-done`. WorkEndAt is stamped and BackgroundPID
// is cleared. There is no artifact gate: cursor-agent edits files in
// place during work, so the reaper cannot tell success from failure
// without re-running it; failures surface via the captured agent log.
func finaliseWorkReap(t store.Task) store.Task {
	end := time.Now().UTC()
	t.WorkEndAt = &end
	t.Status = store.StatusWorkDone
	t.BackgroundPID = 0
	return t
}
