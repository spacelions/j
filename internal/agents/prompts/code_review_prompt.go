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
// external comments. The trailing clarification line is appended via
// appendClarification so a stuck planner has the same escape hatch
// every other role enjoys.
//
// The embedded instructions.CodeReview body carries five %q
// placeholders in this order: review.toml path (read), canonical
// requirements.md path, canonical plan.md path, round plan.md
// output, review.toml path (write).
func CodeReviewPrompt(req codingagents.CodeReviewRequest) string {
	body := fmt.Sprintf(
		strings.TrimSpace(instructions.CodeReview),
		req.ReviewTOMLPath,
		req.RequirementsPath,
		req.PlanPath,
		req.RoundPlanOutputPath,
		req.ReviewTOMLPath,
	)
	return appendClarification(body, req.ClarificationPath)
}
