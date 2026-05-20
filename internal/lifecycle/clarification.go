package lifecycle

import (
	"path/filepath"

	"github.com/spacelions/j/internal/store/tasks"
)

func taskClarificationPresent(taskID string) bool {
	// A failed DefaultDir lookup flows through as "" so the joined
	// path becomes just taskID; ClarificationFileExists then returns
	// false (the file doesn't exist anywhere outside the per-task
	// dir), which is the same outcome we'd otherwise return early.
	tasksDir, _ := tasks.DefaultDir()
	return tasks.ClarificationFileExists(filepath.Join(tasksDir, taskID))
}
