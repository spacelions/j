package plan

import (
	"context"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store/tasks"
)

// UI is the slice of picker methods `j plan` calls. *picker.Picker
// satisfies it via duck typing; tests inject scripted fakes. The
// PromptLinear* / PickLinearProject methods are only invoked when
// the source picker hits the Linear branch.
type UI interface {
	SelectSource(ctx context.Context, allowed []picker.Source) (picker.Source, error)
	PickMarkdownInCwd(ctx context.Context) (string, error)
	PickTask(ctx context.Context, title string, tasks []tasks.Task) (string, bool, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
	PromptLinearAPIKey(ctx context.Context, openURL string) (string, bool, error)
	PickLinearProject(ctx context.Context, projects []linear.Project) (linear.Project, bool, error)
	PromptLinearIdentifier(ctx context.Context) (string, bool, error)
}
