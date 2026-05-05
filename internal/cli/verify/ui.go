package verify

import (
	"context"

	"github.com/spacelions/j/internal/store/tasks"
)

// UI is the slice of picker methods `j verify` calls. *picker.Picker
// satisfies it via duck typing; tests inject a scripted fake.
type UI interface {
	PickTask(ctx context.Context, title string, tasks []tasks.Task) (string, bool, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}
