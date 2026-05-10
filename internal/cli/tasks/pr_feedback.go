package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	storetasks "github.com/spacelions/j/internal/store/tasks"
	runutil "github.com/spacelions/j/internal/util/run"
)

const (
	prFeedbackPlanFileName = "pr_comments_summary_plan.md"
	prFeedbackAgentLogName = "pr_feedback_agent.log"
	prFeedbackPhase        = "pr-feedback"
)

// PRFeedbackOptions configures one manual PR command processing run.
type PRFeedbackOptions struct {
	InputPath   string
	Invocation  PRFeedbackInvocation
	Tool        string
	Model       string
	Interactive bool
	Stdout      io.Writer
	Stderr      io.Writer
	Agents      []codingagents.Agent
}

// PRFeedbackInvocation is the JSON payload accepted by
// `j tasks pr-feedback --input`.
type PRFeedbackInvocation struct {
	PullRequestURL       string              `json:"pr_url"`
	PullRequestTitle     string              `json:"pr_title"`
	PullRequestAuthor    string              `json:"pr_author"`
	PullRequestAuthorBot bool                `json:"pr_author_bot"`
	CommentID            string              `json:"comment_id"`
	CommentAuthor        string              `json:"comment_author"`
	CommentAuthorBot     bool                `json:"comment_author_bot"`
	CommentBody          string              `json:"comment_body"`
	Comments             []PRFeedbackComment `json:"comments"`
}

// PRFeedbackComment is one PR review/comment item included in the
// planner's untrusted feedback context.
type PRFeedbackComment struct {
	ID       string `json:"id"`
	Author   string `json:"author"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	Resolved bool   `json:"resolved"`
}

// RunPRFeedback processes one manual `@j take a look` command. All
// expected accepted/ignored/rejected outcomes are printed to stdout;
// transport and filesystem failures still return errors.
func RunPRFeedback(ctx context.Context, opts PRFeedbackOptions) error {
	opts = opts.withDefaults()
	inv, err := opts.loadInvocation()
	if err != nil {
		return err
	}
	if outcome, ok := preflightPRFeedback(inv); !ok {
		fmt.Fprintf(opts.Stdout, "J: PR command %s: %s\n",
			outcome.kind, outcome.reason)
		return nil
	}
	tasksDir, _ := storetasks.DefaultDir()
	s := storetasks.Open(tasksDir)
	defer func() { _ = s.Close() }()
	matches, err := tasksByPRURL(s, inv.PullRequestURL)
	if err != nil {
		return err
	}
	return runPRFeedbackForMatches(ctx, opts, inv, s, matches)
}

func (o PRFeedbackOptions) withDefaults() PRFeedbackOptions {
	if o.Stdout == nil {
		o.Stdout = io.Discard
	}
	if o.Stderr == nil {
		o.Stderr = io.Discard
	}
	return o
}

func (o PRFeedbackOptions) loadInvocation() (PRFeedbackInvocation, error) {
	if o.InputPath == "" {
		return o.Invocation, nil
	}
	data, err := os.ReadFile(o.InputPath)
	if err != nil {
		return PRFeedbackInvocation{}, fmt.Errorf("pr feedback: read input: %w", err)
	}
	var inv PRFeedbackInvocation
	if err := json.Unmarshal(data, &inv); err != nil {
		return PRFeedbackInvocation{}, fmt.Errorf(
			"pr feedback: decode input: %w", err)
	}
	return inv, nil
}

type prFeedbackOutcome struct {
	kind   string
	reason string
}

func preflightPRFeedback(inv PRFeedbackInvocation) (prFeedbackOutcome, bool) {
	if !isTakeALookCommand(inv.CommentBody) {
		return prFeedbackOutcome{"ignored", "invalid command"}, false
	}
	if strings.TrimSpace(inv.CommentID) == "" {
		return prFeedbackOutcome{"rejected", "invalid command id"}, false
	}
	if isBotUser(inv.PullRequestAuthor, inv.PullRequestAuthorBot) ||
		isBotUser(inv.CommentAuthor, inv.CommentAuthorBot) {
		return prFeedbackOutcome{"rejected", "bot users are not allowed"}, false
	}
	if !sameLogin(inv.PullRequestAuthor, inv.CommentAuthor) {
		return prFeedbackOutcome{"rejected", "unauthorized author"}, false
	}
	return prFeedbackOutcome{"accepted", ""}, true
}

func runPRFeedbackForMatches(
	ctx context.Context,
	opts PRFeedbackOptions,
	inv PRFeedbackInvocation,
	s *storetasks.Store,
	matches []storetasks.Task,
) error {
	switch len(matches) {
	case 0:
		fmt.Fprintln(opts.Stdout, "J: PR command rejected: no matching task")
		return nil
	case 1:
	default:
		fmt.Fprintln(opts.Stdout, "J: PR command rejected: ambiguous task")
		return nil
	}
	task := matches[0]
	if hasProcessedCommand(task, inv.CommentID) {
		fmt.Fprintln(opts.Stdout, "J: PR command rejected: duplicate command")
		return nil
	}
	if taskRunning(task) {
		fmt.Fprintln(opts.Stdout, "J: PR command rejected: locked/running task")
		return nil
	}
	return runLockedPRFeedback(ctx, opts, inv, s, task)
}

func runLockedPRFeedback(
	ctx context.Context,
	opts PRFeedbackOptions,
	inv PRFeedbackInvocation,
	s *storetasks.Store,
	task storetasks.Task,
) error {
	lock, err := storetasks.AcquireLock(
		storetasks.WithPhase(ctx, prFeedbackPhase), task.ID)
	if err != nil {
		var locked *storetasks.LockedError
		if errors.As(err, &locked) {
			fmt.Fprintln(opts.Stdout,
				"J: PR command rejected: locked/running task")
			return nil
		}
		return err
	}
	defer func() { _ = lock.Release() }()
	artifact, err := runPRFeedbackPlanner(ctx, opts, inv, s, task)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "J: PR command accepted: task %s\n", task.ID)
	fmt.Fprintf(opts.Stdout, "J: wrote %s\n", artifact)
	return nil
}

func runPRFeedbackPlanner(
	ctx context.Context,
	opts PRFeedbackOptions,
	inv PRFeedbackInvocation,
	s *storetasks.Store,
	task storetasks.Task,
) (string, error) {
	taskDir := filepath.Join(s.Dir(), task.ID)
	artifact := filepath.Join(taskDir, prFeedbackPlanFileName)
	agent, model, err := resolver.Agent(ctx, resolver.AgentOptions{
		Bucket:        store.BucketPlanner,
		Agents:        opts.Agents,
		ExplicitTool:  opts.Tool,
		ExplicitModel: opts.Model,
		Stderr:        opts.Stderr,
		Interactive:   opts.Interactive,
	})
	if err != nil {
		return "", err
	}
	req := buildPRFeedbackPlanRequest(inv, taskDir, artifact, model)
	pid, err := agent.Plan(ctx, req)
	if err != nil {
		return "", err
	}
	if pid > 0 {
		if err := runutil.WaitForExit(ctx, pid); err != nil {
			return "", err
		}
	}
	body, err := os.ReadFile(artifact)
	if err != nil {
		return "", fmt.Errorf("pr feedback: read artifact: %w", err)
	}
	postPRFeedbackToLinear(ctx, opts.Stderr, task, string(body))
	task.ProcessedPRCommands = append(task.ProcessedPRCommands, inv.CommentID)
	if err := s.PutTask(task); err != nil {
		return "", err
	}
	return artifact, nil
}

func buildPRFeedbackPlanRequest(
	inv PRFeedbackInvocation,
	taskDir, artifact, model string,
) codingagents.PlanRequest {
	return codingagents.PlanRequest{
		TaskDir:        taskDir,
		FromFilePath:   filepath.Join(taskDir, storetasks.RequirementsFileName),
		Model:          model,
		PlanOutputPath: artifact,
		ClarificationPath: filepath.Join(
			taskDir, storetasks.ClarificationFileName,
		),
		AgentLogPath: filepath.Join(taskDir, prFeedbackAgentLogName),
		PRFeedback:   prFeedbackContext(inv),
	}
}

func prFeedbackContext(
	inv PRFeedbackInvocation,
) *codingagents.PRFeedbackContext {
	comments := make([]codingagents.PRFeedbackComment, 0, len(inv.Comments))
	for _, c := range inv.Comments {
		comments = append(comments, codingagents.PRFeedbackComment{
			ID:       c.ID,
			Author:   c.Author,
			Body:     c.Body,
			URL:      c.URL,
			Resolved: c.Resolved,
		})
	}
	return &codingagents.PRFeedbackContext{
		PullRequestURL:        inv.PullRequestURL,
		PullRequestTitle:      inv.PullRequestTitle,
		PullRequestAuthor:     inv.PullRequestAuthor,
		InvocationCommentID:   inv.CommentID,
		InvocationCommentBody: inv.CommentBody,
		Comments:              comments,
	}
}
