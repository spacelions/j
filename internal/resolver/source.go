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
