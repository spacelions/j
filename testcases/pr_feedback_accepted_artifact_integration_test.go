package testcases_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	storetasks "github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestPRFeedback_AcceptedCommandWritesArtifactAcceptance verifies the
// primary acceptance criterion: when @j take a look is accepted, the
// planner produces pr_comments_summary_plan.md inside the task
// directory and does NOT overwrite requirements.md or plan.md.
func TestPRFeedback_AcceptedCommandWritesArtifactAcceptance(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPRFeedbackTaskWithPR(
		t,
		"https://github.com/o/r/pull/1",
		storetasks.StatusWorkDone,
	)

	payload := prFeedbackPayload{
		PRURL:         "https://github.com/o/r/pull/1",
		PRTitle:       "Add feature",
		PRAuthor:      "alice",
		CommentID:     "c1",
		CommentAuthor: "alice",
		CommentBody:   "@j take a look",
		Comments: []prFeedbackCommentPayload{{
			ID: "r1", Author: "reviewer",
			Body: "Please add a test.",
			URL:  "https://github.com/o/r/pull/1#r1",
		}},
	}

	path := writePRPayload(t, payload)
	agent := &artifactWritingAgent{
		body: "Summary\n\nDecision: changes needed\n",
	}
	stdout, _, err := runPRFeedbackWithAgent(t, path, agent)
	if err != nil {
		t.Fatalf("RunPRFeedback: %v", err)
	}
	if !strings.Contains(stdout, "accepted: task "+id) {
		t.Fatalf("stdout = %q, want accepted task %s", stdout, id)
	}

	taskDir := filepath.Join(mustTasksDir(t), id)
	body, err := os.ReadFile(
		filepath.Join(taskDir, "pr_comments_summary_plan.md"),
	)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !strings.Contains(string(body), "Decision: changes needed") {
		t.Fatalf("artifact = %q", string(body))
	}
	planBody, err := os.ReadFile(
		filepath.Join(taskDir, storetasks.PlanFileName),
	)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	if !strings.Contains(string(planBody), "normal plan") {
		t.Fatal("plan.md was overwritten")
	}
	// Verify processed command was persisted.
	row := testutil.ReadTaskRow(t, id)
	if !slices.Contains(row.ProcessedPRCommands, "c1") {
		t.Fatal("command id was not persisted")
	}
}

func seedPRFeedbackTaskWithPR(
	t *testing.T,
	prURL string,
	status storetasks.TaskStatus,
) string {
	t.Helper()
	id := storetasks.NewTaskID()
	taskDir, err := storetasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(
		filepath.Join(taskDir, storetasks.RequirementsFileName), "req",
	); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := testutil.WriteFile(
		filepath.Join(taskDir, storetasks.PlanFileName), "normal plan",
	); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	testutil.SeedTaskRow(t, storetasks.Task{
		ID:             id,
		Status:         status,
		Summary:        "seed",
		PullRequestURL: prURL,
	})
	return id
}

type prFeedbackPayload struct {
	PRURL            string                     `json:"pr_url"`
	PRTitle          string                     `json:"pr_title"`
	PRAuthor         string                     `json:"pr_author"`
	PRAuthorBot      bool                       `json:"pr_author_bot"`
	CommentID        string                     `json:"comment_id"`
	CommentAuthor    string                     `json:"comment_author"`
	CommentAuthorBot bool                       `json:"comment_author_bot"`
	CommentBody      string                     `json:"comment_body"`
	Comments         []prFeedbackCommentPayload `json:"comments"`
}

type prFeedbackCommentPayload struct {
	ID       string `json:"id"`
	Author   string `json:"author"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	Resolved bool   `json:"resolved"`
}

func writePRPayload(
	t *testing.T, p prFeedbackPayload,
) string {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	return path
}

func runPRFeedbackWithAgent(
	t *testing.T,
	inputPath string,
	agent codingagents.Agent,
) (string, string, error) {
	t.Helper()
	var stdout, stderr strings.Builder
	err := tasks.RunPRFeedback(t.Context(), tasks.PRFeedbackOptions{
		InputPath: inputPath,
		Tool:      "scripted",
		Model:     "m1",
		Stdout:    &stdout,
		Stderr:    &stderr,
		Agents:    []codingagents.Agent{agent},
	})
	return stdout.String(), stderr.String(), err
}

type artifactWritingAgent struct {
	body string
}

func (a *artifactWritingAgent) Name() string { return "scripted" }

func (a *artifactWritingAgent) ListModels(
	context.Context,
) ([]string, error) {
	return []string{"m1"}, nil
}

func (a *artifactWritingAgent) CheckLogin(context.Context) error {
	return nil
}

func (a *artifactWritingAgent) NewResumeID(
	context.Context,
) (string, error) {
	return "", nil
}

func (a *artifactWritingAgent) Plan(
	_ context.Context,
	req codingagents.PlanRequest,
) (int, error) {
	return 0, os.WriteFile(
		req.PlanOutputPath, []byte(a.body), 0o644,
	)
}

func (a *artifactWritingAgent) Work(
	context.Context, codingagents.WorkRequest,
) (int, error) {
	return 0, nil
}

func (a *artifactWritingAgent) Verify(
	context.Context, codingagents.VerifyRequest,
) (int, error) {
	return 0, nil
}

func (a *artifactWritingAgent) FormatLog(line []byte) []byte {
	return line
}

func mustInit(t *testing.T) {
	t.Helper()
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
}

func mustTasksDir(t *testing.T) string {
	t.Helper()
	d, err := storetasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	return d
}
