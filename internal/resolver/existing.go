package resolver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/store/tasks"
)

// StartTargetFromExistingTask resolves a task by ID and prepares it
// for re-planning. For markdown-sourced tasks it verifies that
// requirements.md is present; for Linear-sourced tasks it refreshes
// requirements.md from the live issue. Returns StartTarget{IsNew:
// false} so the caller skips the write step.
func StartTargetFromExistingTask(
	ctx context.Context, taskID string,
) (StartTarget, error) {
	task, err := TaskByID(taskID)
	if err != nil {
		return StartTarget{}, err
	}
	tasksDir := tasks.DefaultDir()
	reqPath := filepath.Join(tasksDir, taskID, tasks.RequirementsFileName)
	if task.LinearIssue != "" {
		body, _, fetchErr := FetchLinearBody(ctx, task.LinearIssue)
		if fetchErr != nil {
			return StartTarget{}, fetchErr
		}
		if writeErr := os.WriteFile(reqPath, []byte(body), 0o644); writeErr != nil {
			return StartTarget{}, writeErr
		}
		return StartTarget{TaskID: task.ID, IsNew: false}, nil
	}
	if _, statErr := os.Stat(reqPath); errors.Is(statErr, os.ErrNotExist) {
		return StartTarget{}, fmt.Errorf(
			"task %q has no requirements.md; cannot re-plan", taskID)
	}
	return StartTarget{TaskID: task.ID, IsNew: false}, nil
}
