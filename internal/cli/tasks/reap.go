package tasks

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/run"
)

func reapBackgroundTasks(s *tasks.Store, stderr io.Writer,
	tasksDir string, in []tasks.Task,
) []tasks.Task {
	out := make([]tasks.Task, len(in))
	for i, t := range in {
		out[i] = maybeReap(s, stderr, tasksDir, t)
	}
	return out
}

func maybeReap(s *tasks.Store, stderr io.Writer, tasksDir string,
	t tasks.Task,
) tasks.Task {
	if t.BackgroundPID == 0 {
		return t
	}
	if run.IsAlive(t.BackgroundPID) {
		return t
	}
	switch t.Status {
	case tasks.StatusPlanning:
		return finalisePlanReap(s, stderr, tasksDir, t)
	case tasks.StatusWorking:
		return finaliseWorkReap(s, stderr, tasksDir, t)
	}
	return t
}

func finalisePlanReap(s *tasks.Store, stderr io.Writer, tasksDir string,
	t tasks.Task,
) tasks.Task {
	taskDir := filepath.Join(tasksDir, t.ID)
	t.PlanEndAt = time.Now().UTC()
	t.BackgroundPID = 0

	clarPath := filepath.Join(taskDir, tasks.ClarificationFileName)
	if _, err := os.Stat(clarPath); err == nil {
		applyAndWarn(s, stderr, &t,
			tasks.EventReaperPlanNeedsClarification)
		return t
	}

	requirementsPath := filepath.Join(taskDir, tasks.RequirementsFileName)
	planPath := filepath.Join(taskDir, tasks.PlanFileName)
	reqData, reqErr := os.ReadFile(requirementsPath)
	_, planErr := os.Stat(planPath)
	if reqErr != nil || planErr != nil {
		applyAndWarn(s, stderr, &t, tasks.EventReaperPlanFail)
		return t
	}

	approval, _ := store.LoadPlanRequiresApproval()
	ev := tasks.EventReaperPlanDone
	if approval {
		ev = tasks.EventReaperPlanAwaitApproval
	}
	if summary := tasks.SummarizeMarkdown(string(reqData)); summary != "" {
		t.Summary = summary
	}
	applyAndWarn(s, stderr, &t, ev)
	return t
}

func finaliseWorkReap(s *tasks.Store, stderr io.Writer, tasksDir string,
	t tasks.Task,
) tasks.Task {
	taskDir := filepath.Join(tasksDir, t.ID)
	t.WorkEndAt = time.Now().UTC()
	t.BackgroundPID = 0

	clarPath := filepath.Join(taskDir, tasks.ClarificationFileName)
	if _, err := os.Stat(clarPath); err == nil {
		applyAndWarn(s, stderr, &t,
			tasks.EventReaperWorkNeedsClarification)
		return t
	}
	applyAndWarn(s, stderr, &t, tasks.EventReaperWorkDone)
	return t
}

// applyAndWarn drives the row through ApplyAndPersist and surfaces
// any error as a warning. Every reaper event is legal from its
// source status so in practice only PutTask failures reach the
// warning branch; an FSM-error here would mean the transition table
// got out of sync and is loud-by-design rather than silently dropped.
func applyAndWarn(s *tasks.Store, stderr io.Writer, t *tasks.Task,
	ev tasks.Event,
) {
	if _, err := tasks.ApplyAndPersist(s, t, ev); err != nil {
		uitheme.DangerousDialogBox(stderr, "J: tasks put: %v", err)
	}
}
