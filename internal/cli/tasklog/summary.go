package tasklog

import (
	"path/filepath"

	"github.com/spacelions/j/internal/store"
)

// Summary picks a one-line summary in this order:
//  1. first non-empty line of the requirement / plan markdown,
//  2. the requirement file basename when the body was unreadable.
//
// Truncation is delegated to store.SummarizeMarkdown for the body
// path; the basename path is short by construction. Shared by the
// plan and work phases (work wraps it via FromPlanAndRequirement to
// add the plan-body fallback).
func Summary(requirement, target string) string {
	if s := store.SummarizeMarkdown(requirement); s != "" {
		return s
	}
	if target != "" {
		return filepath.Base(target)
	}
	return ""
}

// PickSource returns whichever of the refined requirements or the
// plan body has a usable first non-empty line, preferring the
// requirements summary because that is the document the agent
// rewrote to capture user intent. Both empty falls through to the
// file basename in Summary.
func PickSource(refinedRequirements, planMarkdown string) string {
	if store.SummarizeMarkdown(refinedRequirements) != "" {
		return refinedRequirements
	}
	return planMarkdown
}

// FromPlanAndRequirement mirrors Summary's precedence for `j work`:
// requirement first, plan body second, file basename last. Kept
// separate from Summary so the plan flow (which only has one body
// candidate at begin time) does not need to pass an empty plan body
// just to reuse the work-flow fallback chain.
func FromPlanAndRequirement(requirement, planBody, planPath string) string {
	if s := store.SummarizeMarkdown(requirement); s != "" {
		return s
	}
	if s := store.SummarizeMarkdown(planBody); s != "" {
		return s
	}
	if planPath != "" {
		return filepath.Base(planPath)
	}
	return ""
}
