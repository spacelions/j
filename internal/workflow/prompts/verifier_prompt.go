package prompts

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/workflow/agents/verifier"
)

// BuildVerifier composes the verifier's shared instruction with the
// requirement / plan markdown bodies and the two output paths the
// agent must write before exiting (verifier_plan.md and
// verifier_findings.md). Reusing verifier.Instruction keeps the
// review rules in a single source of truth across every backend,
// mirroring how BuildPlanner reuses planner.Instruction and
// BuildCoder reuses coder.Instruction.
func BuildVerifier(reqPath, reqBody, planPath, planBody, verifierPlanPath, findingsPath string) string {
	return fmt.Sprintf(
		"%s\n\n"+
			"Requirements (from %q):\n%s\n\n"+
			"Plan (from %q):\n%s\n\n"+
			"Save your draft verification plan to %q (overwrite if it exists) "+
			"and your final findings (with the terminal `VERDICT: PASS` or "+
			"`VERDICT: FAIL` line) to %q (overwrite if it exists). "+
			"Then exit.",
		strings.TrimSpace(verifier.Instruction),
		reqPath, reqBody,
		planPath, planBody,
		verifierPlanPath,
		findingsPath,
	)
}

// BuildVerifierResume composes the resume-only verifier prompt: it
// asks the agent to inspect the previous verification session, check
// what was already done, summarise the prior progress for the user,
// and then continue only the outstanding verification work. The
// requirement / plan paths and bodies are embedded for context only
// — there is no instruction to re-verify from scratch and no
// embedded verifier.Instruction body, mirroring BuildPlannerResume /
// BuildCoderResume.
func BuildVerifierResume(reqPath, reqBody, planPath, planBody string) string {
	return fmt.Sprintf(
		"You are resuming a previous verification session. "+
			"Check what was already done in the previous turn, "+
			"summarise the prior progress for the user in one short paragraph, "+
			"and then continue only the verification work that is still outstanding. "+
			"Do not re-verify from scratch and do not overwrite the saved "+
			"verifier_plan.md / verifier_findings.md unless new information forces a change.\n\n"+
			"Requirements (from %q), provided for context only:\n%s\n\n"+
			"Plan (from %q), provided for context only:\n%s",
		reqPath, reqBody,
		planPath, planBody,
	)
}

// BuildVerifierFix composes the coder-side fix prompt used when the
// outer verify loop has observed a `VERDICT: FAIL` from the verifier
// and wants the previous coder session to address the listed
// findings without re-planning. The plan path and body are embedded
// for context, the findings path and body provide the action items.
//
// As with the resume builders this does NOT include the full coder
// instruction body; the resumed session was already seeded with the
// coding rules on its first run.
func BuildVerifierFix(planPath, planBody, findingsPath, findingsBody string) string {
	return fmt.Sprintf(
		"You are resuming a previous coding session. "+
			"The verifier reviewed your work and reported `VERDICT: FAIL`. "+
			"Read the findings below and address every listed issue by "+
			"editing the project files in place. Do not re-plan from scratch, "+
			"do not edit the verifier's findings file, and keep the change "+
			"set focused on the reported issues.\n\n"+
			"Plan (from %q), provided for context only:\n%s\n\n"+
			"Verifier findings (from %q):\n%s",
		planPath, planBody,
		findingsPath, findingsBody,
	)
}
