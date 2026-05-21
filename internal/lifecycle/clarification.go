package lifecycle

import (
	"path/filepath"

	"github.com/spacelions/j/internal/store/tasks"
)

func taskClarificationPresent(taskID string) bool {
	tasksDir := tasks.DefaultDir()
	return tasks.ClarificationFileExists(filepath.Join(tasksDir, taskID))
}
