package resolver

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store/tasks"
)

// StartUI is the picker surface RunStart needs. Mirrors
// picker.SourceUI verbatim so a *picker.Picker drops in directly;
// scripted fakes in cli/tasks satisfy the same interface.
type StartUI interface {
	SelectSource(
		ctx context.Context, allowed []picker.Source,
	) (picker.Source, error)
	PickMarkdownInCwd(ctx context.Context) (string, error)
	PickTask(
		ctx context.Context, title string, tasks []tasks.Task,
	) (string, bool, error)
	PromptLinearAPIKey(
		ctx context.Context, openURL string,
	) (string, bool, error)
	PickLinearProject(
		ctx context.Context, projects []linear.Project,
	) (linear.Project, bool, error)
	PickLinearIssue(
		ctx context.Context, issues []linear.Issue,
	) (linear.Issue, bool, error)
}

// ResolveStartTarget drives the source-picker chain for `j tasks
// start`. The --from-file shortcut bypasses the picker. Linear
// surfaces as a StartTarget with IsNew=true and Body populated by
// linear.IssueToMarkdown so PrepareStartTaskFiles writes
// requirements.md from memory without a temporary file. Cancelled
// pickers return a zero StartTarget so the caller exits cleanly
// without minting a task.
func ResolveStartTarget(
	ctx context.Context, ui StartUI, _ io.Writer, fromFile string,
) (StartTarget, error) {
	if fromFile != "" {
		return NewStartTargetFromMarkdown(fromFile)
	}
	res, err := picker.PickSource(ctx, ui,
		[]picker.Source{
			picker.SourceLinear,
			picker.SourceMarkdown,
			picker.SourceTask,
		},
		ListAllTasks,
		errors.New("tasks: no tasks to re-plan; "+
			"run `j tasks start --from-file <md>` first"))
	if err != nil {
		return StartTarget{}, err
	}
	if res.Cancelled {
		return StartTarget{}, nil
	}
	switch res.Source {
	case picker.SourceMarkdown:
		return NewStartTargetFromMarkdown(res.Markdown)
	case picker.SourceTask:
		return StartTargetFromExistingTask(ctx, res.TaskID)
	case picker.SourceLinear:
		return StartTargetFromLinear(ctx, res.LinearIdentifier)
	}
	return StartTarget{}, fmt.Errorf(
		"tasks: unsupported source %s", res.Source)
}

// StartTargetFromLinear loads the Linear API key, fetches the issue,
// and returns an in-memory StartTarget whose Body is the markdown
// rendering of the issue. The cli wires this into the --from-linear
// flag and the picker's Linear branch. The upstream identifier is
// recorded on the returned StartTarget so the row's linear_issue
// field round-trips through `j tasks start`.
func StartTargetFromLinear(
	ctx context.Context, identifier string,
) (StartTarget, error) {
	body, sourceLabel, err := FetchLinearBody(ctx, identifier)
	if err != nil {
		return StartTarget{}, err
	}
	return NewStartTargetFromBody(body, sourceLabel, identifier), nil
}

// FetchLinearBody resolves identifier to the markdown body `j` writes
// to requirements.md. Used by both `j tasks start` and `j plan` so
// the auth + fetch + render path is shared.
func FetchLinearBody(
	ctx context.Context, identifier string,
) (string, string, error) {
	if err := linear.ValidateIdentifier(identifier); err != nil {
		return "", "", err
	}
	token, err := linear.LoadAPIKey()
	if err != nil {
		return "", "", err
	}
	if token == "" {
		return "", "", linear.ErrNoAPIKey
	}
	client := linear.NewClient(token)
	issue, err := client.GetIssue(ctx, identifier)
	if err != nil {
		return "", "", err
	}
	return linear.IssueToMarkdown(issue), "linear:" + identifier, nil
}
