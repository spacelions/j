package lifecycle

import (
	"context"
	"time"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/linear"
)

const linearHookTimeout = 30 * time.Second

type linearHookWarn func(format string, a ...any)

type linearHookRun struct {
	client *linear.Client
	issue  linear.Issue
}

func runLinearHook(
	task tasks.Task, warn linearHookWarn,
	fn func(context.Context, linearHookRun),
) {
	token, ok := loadLinearToken(warn)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), linearHookTimeout)
	defer cancel()
	client := linear.NewClient(token)
	issue, err := client.GetIssue(ctx, task.LinearIssue)
	if err != nil {
		warn("resolve %s: %s", task.LinearIssue, err)
		return
	}
	fn(ctx, linearHookRun{
		client: client,
		issue:  issue,
	})
}

func loadLinearToken(warn linearHookWarn) (string, bool) {
	token, err := linear.LoadAPIKey()
	if err != nil {
		warn("load api key: %s", err)
		return "", false
	}
	if token == "" {
		warn("no API key set")
		return "", false
	}
	return token, true
}
