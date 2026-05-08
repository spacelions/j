package tasks

import (
	"path/filepath"
	"strings"
)

// summaryMaxRunes is the upper bound applied to Task.Summary in
// SummarizeMarkdown. Eighty runes fits a typical terminal column even
// after the ID/status/tool/model prefix, and pinning the value keeps
// the truncation behaviour tested in one place.
const summaryMaxRunes = 80

// SummarizeMarkdown derives Task.Summary from a markdown body: the
// first non-empty line, with leading "#" / space markers stripped and
// the result truncated to summaryMaxRunes runes. Empty input yields
// an empty summary so callers can decide whether to substitute a
// placeholder.
func SummarizeMarkdown(body string) string {
	for raw := range strings.SplitSeq(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "# ")
		return truncateRunes(line, summaryMaxRunes)
	}
	return ""
}

// truncateRunes returns s if it is at most max runes long, otherwise
// the first max runes. Operating on runes (not bytes) keeps multibyte
// UTF-8 input from being cut mid-codepoint.
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

// Summary picks a one-line summary in this order:
//  1. first non-empty line of the requirement / plan markdown,
//  2. the requirement file basename when the body was unreadable.
//
// Truncation is delegated to SummarizeMarkdown for the body path; the
// basename path is short by construction. Shared by the plan and
// work phases (work wraps it via FromPlanAndRequirement to add the
// plan-body fallback).
func Summary(requirement, target string) string {
	if s := SummarizeMarkdown(requirement); s != "" {
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
	if SummarizeMarkdown(refinedRequirements) != "" {
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
	if s := SummarizeMarkdown(requirement); s != "" {
		return s
	}
	if s := SummarizeMarkdown(planBody); s != "" {
		return s
	}
	if planPath != "" {
		return filepath.Base(planPath)
	}
	return ""
}
