package testcases_test

import (
	"context"
	"os"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	storetasks "github.com/spacelions/j/internal/store/tasks"
)

// TestPRFeedback_UnauthorizedAuthorRejectedAcceptance verifies that
// only the PR author can invoke @j take a look. Anyone else is
// rejected with "unauthorized author".
func TestPRFeedback_UnauthorizedAuthorRejectedAcceptance(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedPRFeedbackTaskWithPR(
		t,
		"https://github.com/o/r/pull/1",
		storetasks.StatusWorkDone,
	)

	payload := prFeedbackPayload{
		PRURL:         "https://github.com/o/r/pull/1",
		PRTitle:       "Add feature",
		PRAuthor:      "alice",
		CommentID:     "c1",
		CommentAuthor: "bob",
		CommentBody:   "@j take a look",
	}

	path := writePRPayload(t, payload)
	stdout, _, err := runPRFeedbackWithAgent(t, path, &noopAgent{})
	if err != nil {
		t.Fatalf("RunPRFeedback: %v", err)
	}
	if !strings.Contains(stdout, "rejected: unauthorized author") {
		t.Fatalf("stdout = %q, want unauthorized author", stdout)
	}
}

// TestPRFeedback_InvalidCommandIgnoredAcceptance verifies that
// non-command PR comments are ignored with "ignored: invalid command".
func TestPRFeedback_InvalidCommandIgnoredAcceptance(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedPRFeedbackTaskWithPR(
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
		CommentBody:   "please review this",
	}

	path := writePRPayload(t, payload)
	stdout, _, err := runPRFeedbackWithAgent(t, path, &noopAgent{})
	if err != nil {
		t.Fatalf("RunPRFeedback: %v", err)
	}
	if !strings.Contains(stdout, "ignored: invalid command") {
		t.Fatalf("stdout = %q, want ignored invalid command", stdout)
	}
}

// TestPRFeedback_DuplicateCommandRejectedAcceptance verifies that
// reprocessing the same command does not start duplicate planning.
func TestPRFeedback_DuplicateCommandRejectedAcceptance(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPRFeedbackTaskWithPR(
		t,
		"https://github.com/o/r/pull/1",
		storetasks.StatusWorkDone,
	)

	// First run: accepted.
	payload := prFeedbackPayload{
		PRURL:         "https://github.com/o/r/pull/1",
		PRTitle:       "Add feature",
		PRAuthor:      "alice",
		CommentID:     "c1",
		CommentAuthor: "alice",
		CommentBody:   "@j take a look",
	}

	path := writePRPayload(t, payload)
	agent := &artifactWritingAgent{body: "Summary\n\nDecision: no code changes needed\n"}
	stdout, _, err := runPRFeedbackWithAgent(t, path, agent)
	if err != nil {
		t.Fatalf("first RunPRFeedback: %v", err)
	}
	if !strings.Contains(stdout, "accepted: task "+id) {
		t.Fatalf("first stdout = %q, want accepted", stdout)
	}

	// Second run with the same comment ID: rejected as duplicate.
	path2 := writePRPayload(t, payload)
	stdout2, _, err := runPRFeedbackWithAgent(t, path2, &noopAgent{})
	if err != nil {
		t.Fatalf("second RunPRFeedback: %v", err)
	}
	if !strings.Contains(stdout2, "rejected: duplicate command") {
		t.Fatalf("second stdout = %q, want duplicate command", stdout2)
	}
}

// TestPRFeedback_NoMatchingTaskRejectedAcceptance verifies that a PR
// URL with no matching task returns "rejected: no matching task".
func TestPRFeedback_NoMatchingTaskRejectedAcceptance(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)

	payload := prFeedbackPayload{
		PRURL:         "https://github.com/o/r/pull/99",
		PRTitle:       "Add feature",
		PRAuthor:      "alice",
		CommentID:     "c1",
		CommentAuthor: "alice",
		CommentBody:   "@j take a look",
	}

	path := writePRPayload(t, payload)
	stdout, _, err := runPRFeedbackWithAgent(t, path, &noopAgent{})
	if err != nil {
		t.Fatalf("RunPRFeedback: %v", err)
	}
	if !strings.Contains(stdout, "rejected: no matching task") {
		t.Fatalf("stdout = %q, want no matching task", stdout)
	}
}

// TestPRFeedback_AmbiguousTaskRejectedAcceptance verifies that
// multiple tasks matching the same PR URL are rejected.
func TestPRFeedback_AmbiguousTaskRejectedAcceptance(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedPRFeedbackTaskWithPR(
		t,
		"https://github.com/o/r/pull/1",
		storetasks.StatusWorkDone,
	)
	seedPRFeedbackTaskWithPR(
		t,
		"https://github.com/o/r/pull/1",
		storetasks.StatusCompleted,
	)

	payload := prFeedbackPayload{
		PRURL:         "https://github.com/o/r/pull/1",
		PRTitle:       "Add feature",
		PRAuthor:      "alice",
		CommentID:     "c1",
		CommentAuthor: "alice",
		CommentBody:   "@j take a look",
	}

	path := writePRPayload(t, payload)
	stdout, _, err := runPRFeedbackWithAgent(t, path, &noopAgent{})
	if err != nil {
		t.Fatalf("RunPRFeedback: %v", err)
	}
	if !strings.Contains(stdout, "rejected: ambiguous task") {
		t.Fatalf("stdout = %q, want ambiguous task", stdout)
	}
}

// TestPRFeedback_RunningTaskRejectedAcceptance verifies that a
// task in a running state is rejected.
func TestPRFeedback_RunningTaskRejectedAcceptance(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedPRFeedbackTaskWithPR(
		t,
		"https://github.com/o/r/pull/1",
		storetasks.StatusWorking,
	)

	payload := prFeedbackPayload{
		PRURL:         "https://github.com/o/r/pull/1",
		PRTitle:       "Add feature",
		PRAuthor:      "alice",
		CommentID:     "c1",
		CommentAuthor: "alice",
		CommentBody:   "@j take a look",
	}

	path := writePRPayload(t, payload)
	stdout, _, err := runPRFeedbackWithAgent(t, path, &noopAgent{})
	if err != nil {
		t.Fatalf("RunPRFeedback: %v", err)
	}
	if !strings.Contains(stdout, "rejected: locked/running task") {
		t.Fatalf("stdout = %q, want locked/running task", stdout)
	}
}

// TestPRFeedback_PlannerOnlyNoCodeChangesAcceptance verifies that
// accepted commands only call Plan (never Work or Verify).
func TestPRFeedback_PlannerOnlyNoCodeChangesAcceptance(t *testing.T) {
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
	}

	path := writePRPayload(t, payload)
	agent := &countCallingAgent{}
	stdout, _, err := runPRFeedbackWithAgent(t, path, agent)
	if err != nil {
		t.Fatalf("RunPRFeedback: %v", err)
	}
	if !strings.Contains(stdout, "accepted: task "+id) {
		t.Fatalf("stdout = %q, want accepted", stdout)
	}
	if agent.workCalls != 0 || agent.verifyCalls != 0 {
		t.Fatalf("work=%d verify=%d, want planner only (0,0)",
			agent.workCalls, agent.verifyCalls)
	}
}

type noopAgent struct{}

func (a *noopAgent) Name() string                                 { return "scripted" }
func (a *noopAgent) ListModels(context.Context) ([]string, error) { return []string{"m1"}, nil }
func (a *noopAgent) CheckLogin(context.Context) error             { return nil }
func (a *noopAgent) NewResumeID(context.Context) (string, error)  { return "", nil }
func (a *noopAgent) Plan(context.Context, codingagents.PlanRequest) (int, error) {
	return 0, nil
}

func (a *noopAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	return 0, nil
}

func (a *noopAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, nil
}
func (a *noopAgent) FormatLog(line []byte) []byte { return line }

type countCallingAgent struct {
	workCalls   int
	verifyCalls int
}

func (a *countCallingAgent) Name() string                                 { return "scripted" }
func (a *countCallingAgent) ListModels(context.Context) ([]string, error) { return []string{"m1"}, nil }
func (a *countCallingAgent) CheckLogin(context.Context) error             { return nil }
func (a *countCallingAgent) NewResumeID(context.Context) (string, error)  { return "", nil }
func (a *countCallingAgent) Plan(
	_ context.Context, req codingagents.PlanRequest,
) (int, error) {
	return 0, os.WriteFile(req.PlanOutputPath, []byte("ok"), 0o644)
}

func (a *countCallingAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	a.workCalls++
	return 0, nil
}

func (a *countCallingAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	a.verifyCalls++
	return 0, nil
}
func (a *countCallingAgent) FormatLog(line []byte) []byte { return line }
