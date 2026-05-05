package plan

import (
	"context"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/store/tasks"
)

// UI is the slice of picker methods `j plan` calls. *picker.Picker
// satisfies it via duck typing; tests inject scripted fakes.
type UI interface {
	SelectSource(ctx context.Context, allowed []picker.Source) (picker.Source, error)
	PickMarkdownInCwd(ctx context.Context) (string, error)
	PickTask(ctx context.Context, title string, tasks []tasks.Task) (string, bool, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}
