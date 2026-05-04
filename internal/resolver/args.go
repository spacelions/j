package resolver

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/store"
)

type StatusOverrideUI interface {
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}

func ConfirmStatusOverride(ctx context.Context, ui StatusOverrideUI, yes bool, cmd string, task store.Task, allowed func(store.Task) bool) (bool, error) {
	if allowed(task) || yes {
		return true, nil
	}
	return ui.ConfirmStatusOverride(ctx, cmd, task.ID, string(task.Status))
}

func ReplanAllowed(task store.Task) bool {
	switch task.Status {
	case store.StatusPlanDone, store.StatusHelp:
		return true
	}
	return false
}

func WorkAllowed(task store.Task) bool {
	return ReplanAllowed(task)
}

func VerifyAllowed(task store.Task) bool {
	switch task.Status {
	case store.StatusWorkDone, store.StatusVerifyDone, store.StatusHelp:
		return true
	}
	return false
}

type WorkPlanUI interface {
	PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error)
}

type WorkPlanOptions struct {
	TaskID string
	UI     WorkPlanUI
}

func ResolveWorkPlan(ctx context.Context, opts WorkPlanOptions) (WorkPlan, bool, error) {
	switch {
	case opts.TaskID != "":
		r, err := resolveWorkByTaskID(opts.TaskID)
		return r, err == nil, err
	}
	tasks, err := listResolvableTasks("work")
	if err != nil {
		return WorkPlan{}, false, err
	}
	if len(tasks) == 0 {
		return WorkPlan{}, false, errors.New("J: no tasks to work; run `j plan` first")
	}
	if id, ok := autoPickAllowed(tasks, WorkAllowed); ok {
		r, err := resolveWorkByTaskID(id)
		return r, err == nil, err
	}
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a task to work", tasks)
	if err != nil || !ok {
		return WorkPlan{}, false, err
	}
	r, err := resolveWorkByTaskID(chosen)
	return r, err == nil, err
}

type VerifyTaskUI interface {
	PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error)
}

type VerifyTaskOptions struct {
	TaskID string
	UI     VerifyTaskUI
}

func ResolveVerifyTask(ctx context.Context, opts VerifyTaskOptions) (VerifyTask, bool, error) {
	if opts.TaskID != "" {
		r, err := resolveVerifyByTaskID(opts.TaskID)
		return r, err == nil, err
	}
	tasks, err := listResolvableTasks("verify")
	if err != nil {
		return VerifyTask{}, false, err
	}
	if len(tasks) == 0 {
		return VerifyTask{}, false, errors.New("J: no tasks to verify; run `j plan` and `j work` first")
	}
	if id, ok := autoPickAllowed(tasks, VerifyAllowed); ok {
		r, err := resolveVerifyByTaskID(id)
		return r, err == nil, err
	}
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a task to verify", tasks)
	if err != nil || !ok {
		return VerifyTask{}, false, err
	}
	r, err := resolveVerifyByTaskID(chosen)
	return r, err == nil, err
}

type StartUI interface {
	SelectSource(ctx context.Context, allowed []picker.Source) (picker.Source, error)
	PickMarkdownInCwd(ctx context.Context) (string, error)
	PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error)
}

func ResolveStartTarget(ctx context.Context, ui StartUI, stdout io.Writer, fromFile string) (StartTarget, error) {
	if fromFile != "" {
		return NewStartTargetFromMarkdown(fromFile)
	}
	res, err := picker.PickSource(ctx, ui,
		[]picker.Source{picker.SourceMarkdown, picker.SourceLinear, picker.SourceTask},
		ListAllTasks,
		errors.New("tasks: no tasks to re-plan; run `j tasks start --from-file <md>` first"))
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
		return StartTarget{TaskID: res.TaskID}, nil
	case picker.SourceLinear:
		banner.Fprintln(stdout, "J: tasks linear source is not yet wired up; nothing to do")
		return StartTarget{}, nil
	}
	return StartTarget{}, fmt.Errorf("tasks: unsupported source %s", res.Source)
}
