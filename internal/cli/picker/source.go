package picker

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacelions/j/internal/store/tasks"
)

// Source is the planning input the user picks at the start of a
// new-or-resume flow. Values double as user-facing labels so the
// SelectSource picker and the cli's switch/case use one string
// constant. Each cli decides which subset to surface by passing
// `allowed` to SelectSource / PickSource.
type Source string

const (
	SourceMarkdown Source = "markdown"
	SourceLinear   Source = "linear"
	SourceTask     Source = "existing task"
)

// SourceResult bundles the typed outcome of PickSource: which source
// the user chose plus its resolved value (markdown abs path or task
// id). Linear sources surface as Source=SourceLinear with both
// Markdown and TaskID empty so the cli can print its own "not yet
// wired up" message in one switch arm. Cancelled is true when the
// user aborted at a sub-picker (Ctrl-C / Esc); the Source field still
// reflects which sub-picker fired.
type SourceResult struct {
	Source    Source
	Markdown  string
	TaskID    string
	Cancelled bool
}

// SourceUI is the slice of UI behaviour PickSource needs. *Picker
// satisfies it; cli commands' narrow UI interfaces (plan.UI,
// task.StartUI) include the same three methods so their scripted
// fakes satisfy it too.
type SourceUI interface {
	SelectSource(ctx context.Context, allowed []Source) (Source, error)
	PickMarkdownInCwd(ctx context.Context) (string, error)
	PickTask(ctx context.Context, title string, tasks []tasks.Task) (string, bool, error)
}

// SelectSource renders the top-level source widget over the supplied
// allowed list. A returned Source is guaranteed to be one of `allowed`;
// an empty allowed list surfaces a wrapped error so misuse is loud at
// the call site.
func (p *Picker) SelectSource(ctx context.Context, allowed []Source) (Source, error) {
	if len(allowed) == 0 {
		return "", errors.New("picker: no sources allowed")
	}
	labels := make([]string, len(allowed))
	bySource := make(map[string]Source, len(allowed))
	for i, s := range allowed {
		labels[i] = string(s)
		bySource[string(s)] = s
	}
	chosen, err := p.choose(ctx, "Select plan source", labels)
	if err != nil {
		return "", err
	}
	got, ok := bySource[chosen]
	if !ok {
		return "", fmt.Errorf("picker: unknown source %q", chosen)
	}
	return got, nil
}

// PickSource drives the full source-picker chain in one call: prompt
// for which source, then dispatch into the matching sub-picker. The
// SourceTask branch picks an existing task and returns its id;
// callers decide what that id means in their flow (re-plan, resume,
// inspect, etc.). listTasks is invoked only on that branch; cli's
// that don't allow SourceTask omit it from `allowed` and may pass
// listTasks=nil. A nil listTasks reached on the task branch surfaces
// a misuse error so the bug is loud.
//
// emptyTasksErr is returned when the SourceTask branch fires and
// listTasks returns no rows. Callers supply a flow-specific message
// (e.g. "plan: no tasks to re-plan; run `j plan` first"). Pass nil
// to fall back to a generic "picker: no tasks available".
func PickSource(ctx context.Context, ui SourceUI, allowed []Source, listTasks func() ([]tasks.Task, error), emptyTasksErr error) (SourceResult, error) {
	src, err := ui.SelectSource(ctx, allowed)
	if err != nil {
		return SourceResult{}, err
	}
	switch src {
	case SourceMarkdown:
		path, err := ui.PickMarkdownInCwd(ctx)
		if err != nil {
			return SourceResult{}, err
		}
		return SourceResult{Source: src, Markdown: path}, nil
	case SourceLinear:
		return SourceResult{Source: src}, nil
	case SourceTask:
		if listTasks == nil {
			return SourceResult{}, errors.New("picker: SourceTask requires a listTasks callback")
		}
		tasks, err := listTasks()
		if err != nil {
			return SourceResult{}, err
		}
		if len(tasks) == 0 {
			if emptyTasksErr != nil {
				return SourceResult{}, emptyTasksErr
			}
			return SourceResult{}, errors.New("picker: no tasks available")
		}
		id, ok, err := ui.PickTask(ctx, "Select an existing task", tasks)
		if err != nil {
			return SourceResult{}, err
		}
		if !ok {
			return SourceResult{Source: src, Cancelled: true}, nil
		}
		return SourceResult{Source: src, TaskID: id}, nil
	}
	return SourceResult{}, fmt.Errorf("picker: unsupported source %s", src)
}
