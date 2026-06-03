package prompts

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/agents/instructions"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// CodeReviewPrompt composes the code-review planner prompt out of
// four pieces (role body + IO directive + save suffix + the
// canonical clarification escape hatch), mirroring how the planner
// composes BuildPlannerPrompt / AppendPlannerSaveSuffix:
//
//  1. instructions.CodeReview                — role rules
//  2. instructions.CodeReviewRequest         (fresh) OR
//     instructions.CodeReviewClarificationResume (resume)
//  3. instructions.CodeReviewSaveSuffix      — exit contract
//  4. appendClarification                    — escape hatch
//
// Only step 2 differs between modes; everything else is shared so
// a future tweak to the rules or the save contract lands in
// exactly one file regardless of which IO directive ran.
//
// req.MustRead is prepended via prependMustRead at the very top.
func CodeReviewPrompt(req codingagents.CodeReviewRequest) string {
	composed := composeCodeReviewBody(req)
	composed = appendCodeReviewSaveSuffix(composed, req)
	composed = appendClarification(composed, req.ClarificationPath)
	return prependMustRead(composed, req.MustRead)
}

// composeCodeReviewBody concatenates the shared role body with
// the IO directive matching req.Resume.
func composeCodeReviewBody(req codingagents.CodeReviewRequest) string {
	role := strings.TrimSpace(instructions.CodeReview)
	if req.Resume {
		return role + "\n\n" + buildClarificationResumeDirective(req)
	}
	return role + "\n\n" + buildRequestDirective(req)
}

func buildRequestDirective(req codingagents.CodeReviewRequest) string {
	return fmt.Sprintf(
		strings.TrimSpace(instructions.CodeReviewRequest),
		req.ReviewTOMLPath,
		req.RequirementsPath,
		req.PlanPath,
	)
}

// buildClarificationResumeDirective renders the five-%q resume
// template: clarification.md (read), review.toml, requirements,
// plan, clarification.md (delete-when-resolved).
func buildClarificationResumeDirective(
	req codingagents.CodeReviewRequest,
) string {
	return fmt.Sprintf(
		strings.TrimSpace(instructions.CodeReviewClarificationResume),
		req.ClarificationPath,
		req.ReviewTOMLPath,
		req.RequirementsPath,
		req.PlanPath,
		req.ClarificationPath,
	)
}

// appendCodeReviewSaveSuffix wraps base with the shared exit
// contract. Centralising it (instead of duplicating the
// save-instructions inside both the fresh and resume IO templates)
// matches the planner's AppendPlannerSaveSuffix design.
func appendCodeReviewSaveSuffix(
	base string, req codingagents.CodeReviewRequest,
) string {
	return fmt.Sprintf(
		"%s\n\n"+strings.TrimSpace(instructions.CodeReviewSaveSuffix),
		base,
		req.RoundPlanOutputPath,
		req.ReviewTOMLPath,
	)
}
