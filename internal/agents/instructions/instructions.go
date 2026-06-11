// Package instructions exposes the embedded role-prompt markdown
// (planner / worker / verifier) as Go strings. The package is a
// dependency-free leaf — its only Go import is `embed` — so both
// the prompts renderer and the role agent constructors can share a
// single source of truth without forming an import cycle through
// the cli / coding-agents chain.
package instructions

import _ "embed"

// Planner is the embedded planner system prompt. Used by every
// backend (Cursor, Claude, the ADK LLMAgent) so planning rules
// stay in lockstep.
//
//go:embed planner.md
var Planner string

// Worker is the embedded worker system prompt. Used by every
// backend so coding rules stay in lockstep.
//
//go:embed worker.md
var Worker string

// Verifier is the embedded verifier system prompt. Used by every
// backend so review rules stay in lockstep.
//
//go:embed verifier.md
var Verifier string

// PlannerRequest is the planner's read-the-request tail used by the
// fresh-run BuildPlanner. Carries one %q placeholder for the user
// request markdown path.
//
//go:embed planner_request.md
var PlannerRequest string

// PlannerSaveSuffix is the canonical save-and-exit suffix appended
// to the planner prompt by AppendPlannerSaveSuffix. Carries two %q
// placeholders for the requirements.md and plan.md paths.
//
//go:embed planner_save_suffix.md
var PlannerSaveSuffix string

// PlannerResume is the resume-only planner framing used by
// BuildPlannerResume. Carries one %q placeholder for the original
// user request markdown path.
//
//go:embed planner_resume.md
var PlannerResume string

// WorkerPlan is the worker's read-the-plan tail used by the
// fresh-run BuildWorker. Carries one %q placeholder for the plan
// markdown path.
//
//go:embed worker_plan.md
var WorkerPlan string

// WorkerResume is the resume-only worker framing used by
// BuildWorkerResume. Carries one %q placeholder for the plan
// markdown path.
//
//go:embed worker_resume.md
var WorkerResume string

// WorkerWorktree is the worker's `git worktree add` direction line
// appended by appendWorktreeLine on a non-empty worktree. Carries
// two %q placeholders: the absolute checkout path, then the branch
// name.
//
//go:embed worker_worktree.md
var WorkerWorktree string

// VerifierRequest is the verifier's read-and-save block used by
// BuildVerifier. Carries three %q placeholders for the requirements
// path, the plan path, and the findings output path.
//
//go:embed verifier_request.md
var VerifierRequest string

// VerifierResume is the resume-only verifier framing used by
// BuildVerifierResume. Carries two %q placeholders for the
// requirements path and the plan path.
//
//go:embed verifier_resume.md
var VerifierResume string

// VerifierFix is the fix-loop worker framing used by
// BuildVerifierFix when the verifier reports VERDICT: FAIL. Carries
// two %q placeholders for the plan path and the findings path.
//
//go:embed verifier_fix.md
var VerifierFix string

// VerifierWorktree is the verifier's target-worktree direction line
// appended by appendVerifierWorktreeLine on a non-empty worktree.
// Carries two %q placeholders: the absolute checkout path, then the
// branch name.
//
//go:embed verifier_worktree.md
var VerifierWorktree string

// MustReadHeader is the header line rendered above the bulleted
// must-read project file list. The bullets themselves are produced
// by mustReadSuffix in the prompts package (their content is dynamic).
//
//go:embed mustread_header.md
var MustReadHeader string

// PlannerClarificationResume is the resume-from-clarification planner
// framing used by BuildPlannerClarificationResume. Carries three %q
// placeholders for the per-task clarification.md path (twice — once
// to read, once to delete) and the original user request markdown
// path.
//
//go:embed planner_clarification_resume.md
var PlannerClarificationResume string

// WorkerClarificationResume is the resume-from-clarification worker
// framing used by BuildWorkerClarificationResume. Carries three %q
// placeholders for the per-task clarification.md path (twice — once
// to read, once to delete) and the plan markdown path.
//
//go:embed worker_clarification_resume.md
var WorkerClarificationResume string

// VerifierClarificationResume is the resume-from-clarification
// verifier framing used by BuildVerifierClarificationResume. Carries
// four %q placeholders for the per-task clarification.md path (twice —
// once to read, once to delete), the requirements path, and the plan
// path.
//
//go:embed verifier_clarification_resume.md
var VerifierClarificationResume string

// Clarification is the canonical "if you cannot proceed, write your
// question to <path> and exit" escape hatch every role must honour.
// The prompts package's appendClarification helper renders it once
// at the very end of every composed prompt (planner save suffix,
// worker fresh / resume / fix, verifier fresh / resume) so a custom
// planner.md / worker.md / verifier.md override body cannot drop
// the contract and a future tweak lands in exactly one file.
// Carries one %q placeholder for the per-task clarification.md
// absolute path.
//
//go:embed clarification.md
var Clarification string

// CodeReview is the code-review planner ROLE body. Mirrors the
// planner / worker / verifier roles: it carries the untrusted-
// feedback rule, the forbidden-action list, and the decision
// rules — but no IO directive. The fresh / clarification-resume
// directives and the save suffix are composed on top by
// CodeReviewPrompt, matching how the planner composes
// PlannerRequest / PlannerResume / PlannerSaveSuffix.
//
//go:embed code_review.md
var CodeReview string

// CodeReviewRequest is the fresh-run IO directive: "read
// review.toml at %q; the canonical task spec lives at %q / %q;
// review.toml is pre-populated and you only append decisions."
// Carries three %q placeholders: review.toml, requirements, plan.
//
//go:embed code_review_request.md
var CodeReviewRequest string

// CodeReviewClarificationResume is the resume-from-clarification
// IO directive. Used when codereview.ResolveOrAllocate reuses an
// existing `code-reviews/round-N/` because its `clarification.md`
// is still present. Carries five %q placeholders: clarification.md
// (read), review.toml, requirements, plan, clarification.md
// (delete-when-resolved).
//
//go:embed code_review_clarification_resume.md
var CodeReviewClarificationResume string

// CodeReviewSaveSuffix is the exit contract every code-review
// prompt ends with: save the round plan, rewrite review.toml in
// place, then exit. Carries two %q placeholders: round plan.md
// output path, review.toml output path. Centralising it here
// means a future tweak lands in exactly one file regardless of
// which mode (fresh vs clarification-resume) the planner picked.
//
//go:embed code_review_save_suffix.md
var CodeReviewSaveSuffix string
