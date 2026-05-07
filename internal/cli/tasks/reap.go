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

const clarificationFileName = "clarification.md"

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
		t = finalisePlanReap(tasksDir, t)
	case tasks.StatusWorking:
		t = finaliseWorkReap(tasksDir, t)
	default:
		return t
	}
	if err := s.PutTask(t); err != nil {
		uitheme.DangerousDialogBox(stderr, "J: tasks put: %v", err)
	}
	return t
}

func finalisePlanReap(tasksDir string, t tasks.Task) tasks.Task {
	taskDir := filepath.Join(tasksDir, t.ID)
	t.PlanEndAt = time.Now().UTC()
	t.BackgroundPID = 0

	clarPath := filepath.Join(taskDir, clarificationFileName)
	if _, err := os.Stat(clarPath); err == nil {
		newStatus, fsmErr := tasks.Apply(t.Status,
			tasks.EventReaperPlanNeedsClarification)
		if fsmErr != nil {
			return t
		}
		t.Status = newStatus
		return t
	}

	requirementsPath := filepath.Join(taskDir, tasks.RequirementsFileName)
	planPath := filepath.Join(taskDir, tasks.PlanFileName)
	reqData, reqErr := os.ReadFile(requirementsPath)
	_, planErr := os.Stat(planPath)
	if reqErr != nil || planErr != nil {
		newStatus, fsmErr := tasks.Apply(t.Status,
			tasks.EventReaperPlanFail)
		if fsmErr != nil {
			return t
		}
		t.Status = newStatus
		return t
	}

	approval, _ := store.LoadPlanRequiresApproval()
	ev := tasks.EventReaperPlanDone
	if approval {
		ev = tasks.EventReaperPlanAwaitApproval
	}
	newStatus, err := tasks.Apply(t.Status, ev)
	if err != nil {
		return t
	}
	t.Status = newStatus
	if summary := tasks.SummarizeMarkdown(string(reqData)); summary != "" {
		t.Summary = summary
	}
	return t
}

func finaliseWorkReap(tasksDir string, t tasks.Task) tasks.Task {
	taskDir := filepath.Join(tasksDir, t.ID)
	t.WorkEndAt = time.Now().UTC()
	t.BackgroundPID = 0

	clarPath := filepath.Join(taskDir, clarificationFileName)
	if _, err := os.Stat(clarPath); err == nil {
		newStatus, fsmErr := tasks.Apply(t.Status,
			tasks.EventReaperWorkNeedsClarification)
		if fsmErr != nil {
			return t
		}
		t.Status = newStatus
		return t
	}

	newStatus, err := tasks.Apply(t.Status, tasks.EventReaperWorkDone)
	if err != nil {
		return t
	}
	t.Status = newStatus
	return t
}
