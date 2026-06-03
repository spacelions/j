package tasks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacelions/j/internal/cli/tasks/codereview"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
	"github.com/spacelions/j/internal/tools/github"
)

// seedPlannerBucket writes a (tool, model) pair under the planner
// bucket so resolver.AgentFromStore can resolve the picker-stored
// selection without driving the interactive prompt.
func seedPlannerBucket(t *testing.T, tool, model string) error {
	t.Helper()
	s, err := store.Open(store.DefaultPath())
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	if err := s.Put(store.BucketPlanner, "tool", tool); err != nil {
		return err
	}
	return s.Put(store.BucketPlanner, "model", model)
}

// stubReviewAgent is a coding-agent stub that records the
// CodeReviewRequest it received and writes a planner-style output so
// the validator passes.
type stubReviewAgent struct {
	name       string
	gotReq     codingagents.CodeReviewRequest
	plannerErr error
	writeFile  func(req codingagents.CodeReviewRequest) error
}

func (s *stubReviewAgent) Name() string { return s.name }
func (*stubReviewAgent) ListModels(context.Context) ([]string, error) {
	return nil, nil
}
func (*stubReviewAgent) CheckLogin(context.Context) error { return nil }
func (*stubReviewAgent) NewResumeID(context.Context) (string, error) {
	return "", nil
}

func (*stubReviewAgent) Plan(
	context.Context, codingagents.PlanRequest,
) (int, error) {
	return 0, nil
}

func (*stubReviewAgent) Work(
	context.Context, codingagents.WorkRequest,
) (int, error) {
	return 0, nil
}

func (*stubReviewAgent) Verify(
	context.Context, codingagents.VerifyRequest,
) (int, error) {
	return 0, nil
}
func (*stubReviewAgent) FormatLog(line []byte) []byte { return line }

func (s *stubReviewAgent) CodeReview(
	_ context.Context, req codingagents.CodeReviewRequest,
) (int, error) {
	s.gotReq = req
	if s.plannerErr != nil {
		return 0, s.plannerErr
	}
	if s.writeFile != nil {
		if err := s.writeFile(req); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

// stubReviewFetcher returns canned FetchResult / error pairs without
// touching the network.
type stubReviewFetcher struct {
	res github.FetchResult
	err error
}

func (s *stubReviewFetcher) FetchPR(
	_ context.Context, _ github.PRRef,
) (github.FetchResult, error) {
	return s.res, s.err
}

func setupCodeReviewTask(
	t *testing.T, id, pr string, status tasks.TaskStatus,
) {
	t.Helper()
	t.Chdir(t.TempDir())
	testutil.Init(t)
	s := tasks.OpenDefault()
	require.NoError(t, s.PutTask(tasks.Task{
		ID: id, Status: status, Summary: "x",
		PullRequestURL: pr,
		PlanTool:       "stub", PlanModel: "opus",
	}))
	require.NoError(t, s.Close())
	// Seed canonical artifacts so the prompt request paths exist.
	taskDir := filepath.Join(tasks.DefaultDir(), id)
	require.NoError(t, writeOrTouch(filepath.Join(taskDir, tasks.RequirementsFileName)))
	require.NoError(t, writeOrTouch(filepath.Join(taskDir, tasks.PlanFileName)))
}

func writeOrTouch(path string) error {
	return os.WriteFile(path, []byte("body"), 0o644)
}

// TestNew_HasCodeReviewSubcommand pins registration on `j tasks`.
func TestNew_HasCodeReviewSubcommand(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == cmdCodeReview {
			return
		}
	}
	t.Fatal("expected code-review subcommand")
}

func TestCodeReview_RejectsPositionalArgs(t *testing.T) {
	setupCodeReviewTask(t, "01-t", "https://github.com/x/y/pull/1",
		tasks.StatusWorkDone)
	_, stderr, err := runCommand(t, cmdCodeReview, "extra")
	require.Error(t, err)
	_ = stderr
}

func TestCodeReview_NoArgPicker_FiltersTasksWithPR(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	s := tasks.OpenDefault()
	require.NoError(t, s.PutTask(tasks.Task{
		ID: "01-with-pr", Status: tasks.StatusWorkDone, Summary: "a",
		PullRequestURL: "https://github.com/x/y/pull/1",
	}))
	require.NoError(t, s.PutTask(tasks.Task{
		ID: "02-no-pr", Status: tasks.StatusWorkDone, Summary: "b",
	}))
	require.NoError(t, s.Close())

	ui := &fakeUI{} // empty pickReturn -> cancel
	var stdout, stderr bytes.Buffer
	err := RunCodeReview(t.Context(), CodeReviewOptions{
		Stdout: &stdout, Stderr: &stderr, UI: ui,
		Agents: []codingagents.Agent{&stubReviewAgent{name: "stub"}},
	})
	require.NoError(t, err)
	require.Len(t, ui.lastPickedFrom, 1,
		"picker only sees tasks with a PR URL")
	assert.Equal(t, "01-with-pr", ui.lastPickedFrom[0].ID)
}

func TestCodeReview_NoEligibleTasks(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	var stdout, stderr bytes.Buffer
	err := RunCodeReview(t.Context(), CodeReviewOptions{
		Stdout: &stdout, Stderr: &stderr,
		UI:     &fakeUI{},
		Agents: []codingagents.Agent{&stubReviewAgent{name: "stub"}},
	})
	require.Error(t, err)
	assert.Contains(t, stderr.String(), "no tasks")
}

func TestCodeReview_FromTask_UnknownTask(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	var stderr bytes.Buffer
	err := RunCodeReview(t.Context(), CodeReviewOptions{
		FromTask: "ghost",
		Stderr:   &stderr, UI: &fakeUI{},
		Agents: []codingagents.Agent{&stubReviewAgent{name: "stub"}},
	})
	require.Error(t, err)
}

func TestCodeReview_RejectsTasksWithoutPR(t *testing.T) {
	setupCodeReviewTask(t, "01-t", "", tasks.StatusWorkDone)
	var stderr bytes.Buffer
	err := RunCodeReview(t.Context(), CodeReviewOptions{
		FromTask: "01-t",
		Stderr:   &stderr, UI: &fakeUI{},
		Agents: []codingagents.Agent{&stubReviewAgent{name: "stub"}},
	})
	require.Error(t, err)
	assert.Contains(t, stderr.String(), "no PullRequestURL")
}

func TestCodeReview_RejectsMidFlightStatus(t *testing.T) {
	cases := []tasks.TaskStatus{
		tasks.StatusPlanning, tasks.StatusWorking, tasks.StatusVerifying,
	}
	for _, status := range cases {
		setupCodeReviewTask(t, "01-t",
			"https://github.com/x/y/pull/1", status)
		var stderr bytes.Buffer
		err := RunCodeReview(t.Context(), CodeReviewOptions{
			FromTask: "01-t", Stderr: &stderr, UI: &fakeUI{},
			Agents: []codingagents.Agent{&stubReviewAgent{name: "stub"}},
		})
		require.Error(t, err, "status %s must be rejected", status)
	}
}

func TestCodeReview_NoAgents(t *testing.T) {
	setupCodeReviewTask(t, "01-t", "https://github.com/x/y/pull/1",
		tasks.StatusWorkDone)
	err := RunCodeReview(t.Context(), CodeReviewOptions{
		FromTask: "01-t",
		Stdout:   &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		Agents: nil, UI: &fakeUI{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no coding agents")
}

// --- child tests ---

func TestRunCodeReviewChild_NoTaskID(t *testing.T) {
	err := RunCodeReviewChild(t.Context(), CodeReviewChildOptions{
		Agents: []codingagents.Agent{&stubReviewAgent{name: "stub"}},
	})
	require.Error(t, err)
}

func TestRunCodeReviewChild_NoAgents(t *testing.T) {
	err := RunCodeReviewChild(t.Context(), CodeReviewChildOptions{
		TaskID: "01-t",
	})
	require.Error(t, err)
}

func TestRunCodeReviewChild_FetcherError(t *testing.T) {
	setupCodeReviewTask(t, "01-t", "https://github.com/x/y/pull/1",
		tasks.StatusWorkDone)
	var stderr bytes.Buffer
	err := RunCodeReviewChild(t.Context(), CodeReviewChildOptions{
		TaskID: "01-t", Stderr: &stderr,
		Agents:  []codingagents.Agent{&stubReviewAgent{name: "stub"}},
		Fetcher: &stubReviewFetcher{err: github.ErrUnauthorized},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, github.ErrUnauthorized)
}

// happyChildPlanner returns a writeFile callback that emulates a
// planner that fills in decisions for every fetched item and rewrites
// the round review.toml in place.
func happyChildPlanner(t *testing.T) func(codingagents.CodeReviewRequest) error {
	t.Helper()
	return func(req codingagents.CodeReviewRequest) error {
		f, err := codereview.Load(req.ReviewTOMLPath)
		if err != nil {
			return err
		}
		f.Decision = "changes_needed"
		for i := range f.Items {
			f.Items[i].Decision = "accepted"
			f.Items[i].PlanRef = "P1"
			f.Items[i].Reply = "ack"
		}
		if err := codereview.Save(req.ReviewTOMLPath, f); err != nil {
			return err
		}
		return os.WriteFile(req.RoundPlanOutputPath, []byte("plan"), 0o644)
	}
}

func TestRunCodeReviewChild_Happy(t *testing.T) {
	setupCodeReviewTask(t, "01-t", "https://github.com/x/y/pull/1",
		tasks.StatusWorkDone)
	require.NoError(t, seedPlannerBucket(t, "stub", "opus"))

	stub := &stubReviewAgent{name: "stub", writeFile: happyChildPlanner(t)}
	fetcher := &stubReviewFetcher{res: github.FetchResult{
		PR: github.PR{
			URL: "https://github.com/x/y/pull/1", Owner: "x",
			Repo: "y", Number: 1, State: "open",
		},
		Items: []github.Item{{
			SourceID: "review-comment:1", Kind: github.KindReviewComment,
			Author: "alice", Body: "fix nil",
		}},
	}}
	var stderr bytes.Buffer
	err := RunCodeReviewChild(t.Context(), CodeReviewChildOptions{
		TaskID: "01-t", Stderr: &stderr,
		Agents:  []codingagents.Agent{stub},
		Fetcher: fetcher,
	})
	require.NoError(t, err)
	require.NotEmpty(t, stub.gotReq.ReviewTOMLPath)
	assert.Contains(t, stub.gotReq.ReviewTOMLPath, "code-reviews/round-1/review.toml")
	assert.Contains(t, stub.gotReq.RoundPlanOutputPath, "code-reviews/round-1/plan.md")
}

func TestRunCodeReviewChild_ValidationFails(t *testing.T) {
	setupCodeReviewTask(t, "01-t", "https://github.com/x/y/pull/1",
		tasks.StatusWorkDone)
	require.NoError(t, seedPlannerBucket(t, "stub", "opus"))

	stub := &stubReviewAgent{name: "stub", writeFile: func(req codingagents.CodeReviewRequest) error {
		// Drop fetched source ids so validation fails.
		return codereview.Save(req.ReviewTOMLPath, codereview.ReviewFile{
			SchemaVersion: codereview.SchemaVersion,
			Provider:      "github",
			Decision:      "changes_needed",
		})
	}}
	fetcher := &stubReviewFetcher{res: github.FetchResult{
		PR: github.PR{URL: "u", Owner: "x", Repo: "y", Number: 1, State: "open"},
		Items: []github.Item{{
			SourceID: "review-comment:1",
			Kind:     github.KindReviewComment, Body: "b",
		}},
	}}
	var stderr bytes.Buffer
	err := RunCodeReviewChild(t.Context(), CodeReviewChildOptions{
		TaskID: "01-t", Stderr: &stderr,
		Agents:  []codingagents.Agent{stub},
		Fetcher: fetcher,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing source_id")
}

func TestRunCodeReviewChild_BadPRURL(t *testing.T) {
	setupCodeReviewTask(t, "01-t", "not-a-url", tasks.StatusWorkDone)
	require.NoError(t, seedPlannerBucket(t, "stub", "opus"))
	var stderr bytes.Buffer
	err := RunCodeReviewChild(t.Context(), CodeReviewChildOptions{
		TaskID: "01-t", Stderr: &stderr,
		Agents:  []codingagents.Agent{&stubReviewAgent{name: "stub"}},
		Fetcher: &stubReviewFetcher{},
	})
	require.Error(t, err)
}

func TestRunCodeReviewChild_NoSummaryMD(t *testing.T) {
	setupCodeReviewTask(t, "01-t", "https://github.com/x/y/pull/1",
		tasks.StatusWorkDone)
	require.NoError(t, seedPlannerBucket(t, "stub", "opus"))

	stub := &stubReviewAgent{name: "stub", writeFile: happyChildPlanner(t)}
	fetcher := &stubReviewFetcher{res: github.FetchResult{
		PR:    github.PR{URL: "u", Owner: "x", Repo: "y", Number: 1, State: "open"},
		Items: []github.Item{{SourceID: "review-comment:1", Kind: github.KindReviewComment}},
	}}
	require.NoError(t, RunCodeReviewChild(t.Context(), CodeReviewChildOptions{
		TaskID:  "01-t",
		Stderr:  &bytes.Buffer{},
		Agents:  []codingagents.Agent{stub},
		Fetcher: fetcher,
	}))
	// no summary.md is created under code-reviews/
	taskDir := filepath.Join(tasks.DefaultDir(), "01-t")
	matches, _ := filepath.Glob(filepath.Join(taskDir, "code-reviews", "summary.md"))
	assert.Empty(t, matches)
	// no per-round log either
	matches, _ = filepath.Glob(filepath.Join(taskDir, "code-reviews", "round-1", "agent.log"))
	assert.Empty(t, matches)
}

func TestRunCodeReviewChild_PreservesCanonicalArtifacts(t *testing.T) {
	setupCodeReviewTask(t, "01-t", "https://github.com/x/y/pull/1",
		tasks.StatusWorkDone)
	require.NoError(t, seedPlannerBucket(t, "stub", "opus"))
	taskDir := filepath.Join(tasks.DefaultDir(), "01-t")
	originalReq, _ := os.ReadFile(filepath.Join(taskDir, tasks.RequirementsFileName))
	originalPlan, _ := os.ReadFile(filepath.Join(taskDir, tasks.PlanFileName))

	stub := &stubReviewAgent{name: "stub", writeFile: happyChildPlanner(t)}
	fetcher := &stubReviewFetcher{res: github.FetchResult{
		PR:    github.PR{URL: "u", Owner: "x", Repo: "y", Number: 1, State: "open"},
		Items: []github.Item{{SourceID: "review-comment:1", Kind: github.KindReviewComment}},
	}}
	require.NoError(t, RunCodeReviewChild(t.Context(), CodeReviewChildOptions{
		TaskID:  "01-t",
		Stderr:  &bytes.Buffer{},
		Agents:  []codingagents.Agent{stub},
		Fetcher: fetcher,
	}))
	gotReq, _ := os.ReadFile(filepath.Join(taskDir, tasks.RequirementsFileName))
	gotPlan, _ := os.ReadFile(filepath.Join(taskDir, tasks.PlanFileName))
	assert.Equal(t, string(originalReq), string(gotReq))
	assert.Equal(t, string(originalPlan), string(gotPlan))
	// task.toml status unchanged
	s := tasks.OpenDefault()
	row, err := s.GetTask("01-t")
	_ = s.Close()
	require.NoError(t, err)
	assert.Equal(t, tasks.StatusWorkDone, row.Status)
}

func TestCodeReview_NonCodeReviewerAgentFailsCleanly(t *testing.T) {
	type nonReviewer struct {
		stubReviewAgent
	}
	a := &nonReviewer{stubReviewAgent: stubReviewAgent{name: "no-reviewer"}}
	_, err := codingagents.RunCodeReview(t.Context(), a,
		codingagents.CodeReviewRequest{})
	// stubReviewAgent does implement CodeReviewer through embedding
	// so we wrap a stub that explicitly does not; the easier check
	// is to call RunCodeReview against a stub that does not satisfy.
	_ = err
	// Use a fresh stub that doesn't embed stubReviewAgent.
	plain := &codeReviewPlainAgent{}
	_, err = codingagents.RunCodeReview(t.Context(), plain,
		codingagents.CodeReviewRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support code-review")
}

type codeReviewPlainAgent struct{}

func (codeReviewPlainAgent) Name() string { return "plain" }
func (codeReviewPlainAgent) ListModels(context.Context) ([]string, error) {
	return nil, nil
}
func (codeReviewPlainAgent) CheckLogin(context.Context) error { return nil }
func (codeReviewPlainAgent) NewResumeID(context.Context) (string, error) {
	return "", nil
}

func (codeReviewPlainAgent) Plan(
	context.Context, codingagents.PlanRequest,
) (int, error) {
	return 0, nil
}

func (codeReviewPlainAgent) Work(
	context.Context, codingagents.WorkRequest,
) (int, error) {
	return 0, nil
}

func (codeReviewPlainAgent) Verify(
	context.Context, codingagents.VerifyRequest,
) (int, error) {
	return 0, nil
}
func (codeReviewPlainAgent) FormatLog(line []byte) []byte { return line }
