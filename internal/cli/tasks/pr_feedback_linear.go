package tasks

import (
	"context"
	"fmt"
	"io"
	"time"

	storetasks "github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/linear"
)

func postPRFeedbackToLinear(
	ctx context.Context,
	stderr io.Writer,
	task storetasks.Task,
	body string,
) {
	if task.LinearIssue == "" {
		return
	}
	token, err := linear.LoadAPIKey()
	if err != nil {
		fmt.Fprintf(stderr, "J: linear pr feedback: load api key: %v\n", err)
		return
	}
	if token == "" {
		fmt.Fprintln(stderr, "J: linear pr feedback: no API key set")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := linear.NewClient(token)
	issue, err := client.GetIssue(ctx, task.LinearIssue)
	if err != nil {
		fmt.Fprintf(stderr, "J: linear pr feedback: resolve %s: %v\n",
			task.LinearIssue, err)
		return
	}
	if err := client.CreateComment(ctx, issue.ID, body); err != nil {
		fmt.Fprintf(stderr, "J: linear pr feedback: commentCreate: %v\n", err)
	}
}
