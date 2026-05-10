package tasks

import (
	"context"
	"io"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store/tasks"
)

type ReVerifyOptions struct {
	Stdout io.Writer
	UI     UI
}

func pickReVerifyFromStore(
	ctx context.Context,
	s *tasks.Store,
	opts ReVerifyOptions,
) (string, bool, error) {
	rows, err := s.ListTasks()
	if err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		uitheme.NormalFprintln(opts.Stdout, emptyMessage)
		return "", false, nil
	}
	tasks.SortTasks(rows)
	return opts.UI.PickTask(ctx, rows)
}

func filterTasksWithVerifySession(rows []tasks.Task) []tasks.Task {
	return filterTasksBySession(
		rows,
		func(t tasks.Task) bool { return t.VerifyResumeSession != "" },
	)
}
