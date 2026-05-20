package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spacelions/j/internal/tools/linear"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr,
			"usage: push <issue-identifier> <requirements-path> <plan-path>")
		os.Exit(2)
	}
	id := os.Args[1]
	reqPath := os.Args[2]
	planPath := os.Args[3]

	reqBytes, err := os.ReadFile(reqPath)
	if err != nil {
		fail("read requirements: %v", err)
	}
	planBytes, err := os.ReadFile(planPath)
	if err != nil {
		fail("read plan: %v", err)
	}

	token, err := linear.LoadAPIKey()
	if err != nil {
		fail("load api key: %v", err)
	}
	if token == "" {
		fail("no Linear api key set")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(), 30*time.Second)
	defer cancel()

	client := linear.NewClient(token)
	issue, err := client.GetIssue(ctx, id)
	if err != nil {
		fail("get issue %s: %v", id, err)
	}
	fmt.Fprintf(os.Stderr, "resolved %s -> %s\n", id, issue.ID)

	if err := client.UpdateIssueDescription(
		ctx, issue.ID, string(reqBytes)); err != nil {
		fail("updateIssue description: %v", err)
	}
	fmt.Fprintln(os.Stderr, "description updated")

	if err := client.CreateComment(
		ctx, issue.ID, string(planBytes)); err != nil {
		fail("commentCreate: %v", err)
	}
	fmt.Fprintln(os.Stderr, "comment posted")
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "push: "+format+"\n", a...)
	os.Exit(1)
}
