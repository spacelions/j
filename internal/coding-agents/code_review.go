package codingagents

import (
	"context"
	"fmt"
)

// CodeReviewRequest is the input to CodeReviewer.CodeReview. It is the
// narrow code-review counterpart to PlanRequest: it points the
// backend at the round's review.toml plus the canonical task spec
// (requirements.md + plan.md) for read-only context and names the
// round's plan.md + review.toml output paths the planner writes
// before exiting. No resume/clarification flags are exposed in v1
// because a code-review round is always a fresh planner turn — the
// task's planner session id is preserved separately by the
// orchestrator.
type CodeReviewRequest struct {
	// TaskDir is the per-task `.j/tasks/<id>/` directory. The
	// backend uses it as the workspace (same as Plan) so backend
	// scratch state stays per-task.
	TaskDir string
	// Model is the model identifier resolved from the planner
	// bucket. Empty falls back to the backend default — backends
	// that need a non-empty model are responsible for surfacing the
	// missing-model error themselves.
	Model string
	// ReviewTOMLPath is the absolute path of the round's
	// review.toml. The fetched-feedback portion is already written
	// when the backend runs; the planner appends decisions and
	// rewrites it in place before exiting.
	ReviewTOMLPath string
	// RequirementsPath and PlanPath point at the canonical task
	// spec. The planner reads them as context only — the prompt
	// forbids mutating either.
	RequirementsPath string
	PlanPath         string
	// RoundPlanOutputPath is the round-local `plan.md` the planner
	// writes after deciding on every accepted item. Always under
	// `<task-dir>/code-reviews/round-N/`.
	RoundPlanOutputPath string
	// ClarificationPath is the round-local `clarification.md` the
	// planner writes (and exits) when the feedback cannot be
	// decided without a human answer. Always under
	// `<task-dir>/code-reviews/round-N/`.
	ClarificationPath string
	// Interactive selects the backend's TUI flavour when true and
	// the headless fire-and-forget flavour when false. The
	// code-review cli wires this from `--interactive`.
	Interactive bool
	// AgentLogPath is the per-task `agent.log` the headless
	// backend redirects stdout/stderr to. Same contract as
	// PlanRequest.AgentLogPath.
	AgentLogPath string
}

// CodeReviewer is the optional Agent companion that backs
// `j tasks code-review`. Backends that participate in the planner
// pick must satisfy this interface; backends that opt out (e.g. test
// stubs) trip the explicit error in RunCodeReview instead of
// silently no-oping.
type CodeReviewer interface {
	CodeReview(ctx context.Context, req CodeReviewRequest) (int, error)
}

// RunCodeReview is the type-assertion-aware free helper the cli uses
// to drive a code-review round. Backends that do not satisfy
// CodeReviewer surface as a deterministic error so the cli can
// render the matching dangerous dialog. The signature mirrors
// CaptureResumeID so the cli can call them interchangeably.
func RunCodeReview(
	ctx context.Context, agent Agent, req CodeReviewRequest,
) (int, error) {
	reviewer, ok := agent.(CodeReviewer)
	if !ok {
		return 0, fmt.Errorf(
			"code-review: %s does not support code-review", agent.Name())
	}
	return reviewer.CodeReview(ctx, req)
}
