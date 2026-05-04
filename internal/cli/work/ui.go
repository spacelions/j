package work

import (
	"context"

	"github.com/spacelions/j/internal/store"
)

// UI is the slice of picker methods `j work` calls. *picker.Picker
// satisfies it via duck typing; tests inject a scripted fake. `j work`
// does not surface a top-level source picker (it operates on tasks,
// not free-form markdown), so SelectSource is absent.
type UI interface {
	PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}
