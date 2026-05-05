package picker

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store"
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
// the user chose plus its resolved value. Markdown carries an
// absolute path; TaskID carries an existing task's id; LinearIdentifier
// carries a `<TEAM>-<NUM>` identifier the cli must fetch from Linear.
// Cancelled is true when the user aborted at a sub-picker (Ctrl-C /
// Esc); the Source field still reflects which sub-picker fired.
type SourceResult struct {
	Source           Source
	Markdown         string
	TaskID           string
	LinearIdentifier string
	Cancelled        bool
}

// SourceUI is the slice of UI behaviour PickSource needs. *Picker
// satisfies it; cli commands' narrow UI interfaces (plan.UI,
// tasks.StartUI) include the same methods so their scripted
// fakes satisfy it too. The Linear* methods drive the first-use
// link flow (browser-paste API key + project select) and the
// per-run issue picker; they are only invoked when the user picks
// SourceLinear.
type SourceUI interface {
	SelectSource(ctx context.Context, allowed []Source) (Source, error)
	PickMarkdownInCwd(ctx context.Context) (string, error)
	PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error)
	PromptLinearAPIKey(ctx context.Context, openURL string) (string, bool, error)
	PickLinearProject(ctx context.Context, projects []linear.Project) (linear.Project, bool, error)
	PickLinearIssue(ctx context.Context, issues []linear.Issue) (linear.Issue, bool, error)
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
func PickSource(ctx context.Context, ui SourceUI, allowed []Source, listTasks func() ([]store.Task, error), emptyTasksErr error) (SourceResult, error) {
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
		return pickLinearSource(ctx, ui)
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

// pickLinearSource walks the SourceLinear flow:
//
//  1. First-time link: when no API key is stored, open the browser
//     to Linear's API-keys page and prompt for the pasted token,
//     then save it.
//  2. Default project link: when no project is stored, fetch the
//     project list with the captured token and prompt for one;
//     save the selection. (Skipped silently when the API key has
//     no projects in scope.)
//  3. Issue list: fetch the viewer's open assigned issues — scoped
//     by the saved project when set — and let the user pick one.
//     Empty-list short-circuits with a clear error pointing at
//     `--from-linear` for non-interactive use.
//
// Each prompt honours cancellation (ok=false) by returning a
// Cancelled SourceResult so the caller exits cleanly without
// creating a task. Token / project values are only persisted after
// the user confirms each prompt.
func pickLinearSource(ctx context.Context, ui SourceUI) (SourceResult, error) {
	token, err := linear.LoadAPIKey()
	if err != nil {
		return SourceResult{}, err
	}
	if token == "" {
		t, ok, err := ui.PromptLinearAPIKey(ctx, linear.LinearAPIKeysURL)
		if err != nil {
			return SourceResult{}, err
		}
		if !ok {
			return SourceResult{Source: SourceLinear, Cancelled: true}, nil
		}
		if err := linear.SaveAPIKey(t); err != nil {
			return SourceResult{}, err
		}
		token = t
	}
	project, err := linear.LoadProject()
	if err != nil {
		return SourceResult{}, err
	}
	client := linear.NewClient(token)
	if project == "" {
		projects, err := client.ListProjects(ctx)
		if err != nil {
			return SourceResult{}, err
		}
		if len(projects) > 0 {
			p, ok, err := ui.PickLinearProject(ctx, projects)
			if err != nil {
				return SourceResult{}, err
			}
			if !ok {
				return SourceResult{Source: SourceLinear, Cancelled: true}, nil
			}
			if err := linear.SaveProject(p.ID); err != nil {
				return SourceResult{}, err
			}
			project = p.ID
		}
	}
	issues, err := client.ListAssignedIssues(ctx, linear.ListIssuesOpts{ProjectID: project})
	if err != nil {
		return SourceResult{}, err
	}
	if len(issues) == 0 {
		return SourceResult{}, errors.New("picker: no Linear issues assigned to you (use --from-linear ENG-123 to specify directly, or assign yourself to an issue in Linear)")
	}
	chosen, ok, err := ui.PickLinearIssue(ctx, issues)
	if err != nil {
		return SourceResult{}, err
	}
	if !ok {
		return SourceResult{Source: SourceLinear, Cancelled: true}, nil
	}
	return SourceResult{Source: SourceLinear, LinearIdentifier: chosen.Identifier}, nil
}
