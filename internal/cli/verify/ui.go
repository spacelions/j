package verify

import (
	"context"

	"github.com/spacelions/j/internal/store"
)

// UI is the slice of picker methods `j verify` calls. *picker.Picker
// satisfies it via duck typing; tests inject a scripted fake. The
// interface mirrors `work.UI` exactly (same methods, different titles
// passed to PickTask).
type UI interface {
	AskFromFile(ctx context.Context) (string, error)
	PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}
