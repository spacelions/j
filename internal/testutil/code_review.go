package testutil

import (
	"path/filepath"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// CodeReviewRequest is the per-backend test fixture for
// CodeReviewer.CodeReview. The four backend test files share this
// helper so the per-test scaffolding stays a 3-line setup
// (TempDir + stub binary + helper call) instead of repeating the
// CodeReviewRequest struct literal in each file.
//
// Path fields all anchor to dir so a TempDir is sufficient; values
// match the cli's actual layout (review.toml / plan.md /
// clarification.md / agent.log next to one another) closely enough
// for argv assertions while staying portable across the four
// backends.
func CodeReviewRequest(
	dir, model string, interactive bool,
) codingagents.CodeReviewRequest {
	return codingagents.CodeReviewRequest{
		TaskDir:             dir,
		Model:               model,
		ReviewTOMLPath:      filepath.Join(dir, "review.toml"),
		RequirementsPath:    filepath.Join(dir, "requirements.md"),
		PlanPath:            filepath.Join(dir, "plan.md"),
		RoundPlanOutputPath: filepath.Join(dir, "plan.md"),
		ClarificationPath:   filepath.Join(dir, "clarification.md"),
		Interactive:         interactive,
		AgentLogPath:        filepath.Join(dir, "agent.log"),
	}
}
