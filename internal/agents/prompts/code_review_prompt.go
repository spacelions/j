package prompts

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/agents/instructions"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// CodeReviewPrompt composes the code-review planner prompt. It is
// independent of the canonical planner prompt because the round
// must NOT rewrite `requirements.md` / `plan.md` and must NOT post
// external comments. The trailing clarification line is appended
// via appendClarification so a stuck planner has the same escape
// hatch every other role enjoys.
//
// When req.Resume is true the clarification-resume template is
// rendered instead — used by codereview.ResolveOrAllocate to keep
// the same round when a previous turn left a clarification.md in
// place. The project must_read list is prepended via
// prependMustRead so the planner reads the standard context files
// before deciding on review items.
func CodeReviewPrompt(req codingagents.CodeReviewRequest) string {
	if req.Resume {
		return prependMustRead(
			appendClarification(buildCodeReviewResume(req), req.ClarificationPath),
			req.MustRead,
		)
	}
	return prependMustRead(
		appendClarification(buildCodeReviewFresh(req), req.ClarificationPath),
		req.MustRead,
	)
}

func buildCodeReviewFresh(req codingagents.CodeReviewRequest) string {
	return fmt.Sprintf(
		strings.TrimSpace(instructions.CodeReview),
		req.ReviewTOMLPath,
		req.RequirementsPath,
		req.PlanPath,
		req.RoundPlanOutputPath,
		req.ReviewTOMLPath,
	)
}

// buildCodeReviewResume threads the eight %q slots of
// CodeReviewClarificationResume: clarification.md (read),
// review.toml (read), requirements, plan, clarification.md (delete
// path), clarification.md (rewrite path), round plan output,
// review.toml (write).
func buildCodeReviewResume(req codingagents.CodeReviewRequest) string {
	return fmt.Sprintf(
		strings.TrimSpace(instructions.CodeReviewClarificationResume),
		req.ClarificationPath,
		req.ReviewTOMLPath,
		req.RequirementsPath,
		req.PlanPath,
		req.ClarificationPath,
		req.ClarificationPath,
		req.RoundPlanOutputPath,
		req.ReviewTOMLPath,
	)
}
