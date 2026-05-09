package lifecycle

import (
	"path/filepath"

	"github.com/spacelions/j/internal/store/tasks"
)

func taskClarificationPresent(taskID string) bool {
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return false
	}
	return tasks.ClarificationFileExists(filepath.Join(tasksDir, taskID))
}
