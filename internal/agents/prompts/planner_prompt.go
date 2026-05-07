// Package prompts composes the embedded planner / worker / verifier
// instruction markdown with a user-supplied target so every
// coding-agent backend (Cursor, Codex, Claude, ...) sends the same
// prompt shape. The instruction text lives in the dependency-free
// leaf package internal/agents/instructions (a single package
// that embeds planner.md / worker.md / verifier.md as Planner /
// Worker / Verifier vars), so this package and
// internal/agents/{planner,worker,verifier} share a single source
// of truth without re-introducing the agents → cli →
// coding-agents → prompts → agents import cycle.
package prompts

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/agents/instructions"
)

// BuildPlanner composes the planner's shared instruction with a
// pointer to the user's markdown task. The agent is told to read the
// file from disk rather than receiving its contents inline so the
// prompt stays small and there is no chance of drift between the
// rendered prompt and the on-disk source.
//
// mustRead, when non-empty, is rendered as a bulleted "Before
// starting, read these project files…" block between the instruction
// and the user-request line. An empty / nil mustRead leaves the
// prompt byte-identical to the pre-must-read output.
func BuildPlanner(targetPath string, mustRead []string) string {
	return fmt.Sprintf(
		"%s%s\n\n"+strings.TrimSpace(instructions.PlannerRequest),
		strings.TrimSpace(instructions.Planner),
		mustReadSuffix(mustRead),
		targetPath,
	)
}

// AppendPlannerSaveSuffix wraps base with the canonical
// "save requirements / save plan / then exit" instruction the
// orchestrator expects after either a fresh-run BuildPlanner or a
// resume-run BuildPlannerResume. Centralising the wording here means
// the cursor and claude backends share one source of truth — the
// reaper-visible exit contract is identical across backends and
// across the fresh / resume branches.
//
// The suffix also pins the requirements.md "first line is a one-line
// summary" rule so `j tasks` does not surface the literal heading
// `# Requirements` as a task summary.
func AppendPlannerSaveSuffix(base, requirementsPath, planPath string) string {
	return fmt.Sprintf(
		"%s\n\n"+strings.TrimSpace(instructions.PlannerSaveSuffix),
		base, requirementsPath, planPath,
	)
}

// BuildPlannerResume composes the resume-only planner prompt: it
// asks the agent to inspect the previous session, check what was
// already done, summarise it for the user, and continue only what is
// still outstanding. The original requirement markdown path is
// referenced for context only — there is no instruction to re-plan
// from scratch.
//
// The exit contract (save requirements.md / plan.md and then exit)
// is appended by the backend in buildPlanPrompt — kept there so a
// single save-suffix string is the source of truth for both the
// fresh-run and resume paths and the reaper sees identical
// artifacts in either case.
//
// mustRead, when non-empty, is rendered as a bulleted "Before
// starting, read these project files…" block between the
// instruction and the resume framing line (mirroring BuildPlanner).
// An empty / nil mustRead keeps the prompt byte-identical to the
// pre-must-read output.
//
// The full instructions.Planner body is embedded so the resumed
// session has the same coding rules available as the first-run
// BuildPlanner did. The instruction text itself opens with
// "You are the planner in a planner/worker/verifier workflow.",
// so this builder relies on that opening as the role preamble
// rather than emitting a duplicate sentence.
func BuildPlannerResume(targetPath string, mustRead []string) string {
	return fmt.Sprintf(
		"%s%s\n\n"+strings.TrimSpace(instructions.PlannerResume),
		strings.TrimSpace(instructions.Planner),
		mustReadSuffix(mustRead),
		targetPath,
	)
}
