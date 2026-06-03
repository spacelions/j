package codereview

import (
	"errors"
	"fmt"
	"os"
	"unicode/utf8"
)

// SourceIDSet is the snapshot of fetched `source_id` values the
// validator compares against the planner-updated file. The cli
// takes the snapshot before the planner runs; every fetched id
// appears exactly once in the snapshot, so the post-planner
// equivalent (which would otherwise allow duplicates) is built
// as a per-id count in ValidatePost.
type SourceIDSet map[string]bool

// SnapshotSourceIDs returns the set of source_id values currently
// in f.Items. The cli uses it to pin the fetched-feedback ids
// before passing the file to the planner.
func SnapshotSourceIDs(f ReviewFile) SourceIDSet {
	out := make(SourceIDSet, len(f.Items))
	for _, it := range f.Items {
		out[it.SourceID] = true
	}
	return out
}

// ValidatePost is the post-planner validator. It enforces the
// contract documented in plan.md: every original source_id is
// still present exactly once; no invented ids are tolerated;
// decisions are restricted to the allowed enums; accepted items
// carry a plan_ref; reply text fits inside ReplyMaxRunes.
//
// Duplicate detection (rather than set membership) catches a
// planner that copies an existing source_id into a new row: the
// SourceIDSet check alone would pass because every original id
// is present and no invented id appears, but the round would
// carry the same fetched feedback twice.
func ValidatePost(f ReviewFile, original SourceIDSet) error {
	if !AllowedTopDecisions[f.Decision] {
		return fmt.Errorf(
			"codereview: invalid top-level decision %q", f.Decision)
	}
	if f.Decision == "" {
		return errors.New(
			"codereview: top-level decision is required after planner")
	}
	counts := make(map[string]int, len(f.Items))
	for _, it := range f.Items {
		counts[it.SourceID]++
	}
	for id := range original {
		n, ok := counts[id]
		if !ok {
			return fmt.Errorf("codereview: missing source_id %q", id)
		}
		if n > 1 {
			return fmt.Errorf(
				"codereview: duplicate source_id %q (count %d)", id, n)
		}
	}
	for id := range counts {
		if !original[id] {
			return fmt.Errorf("codereview: invented source_id %q", id)
		}
	}
	for _, it := range f.Items {
		if err := validateItem(it); err != nil {
			return err
		}
	}
	return nil
}

// ValidateRound runs ValidatePost and additionally enforces the
// per-top-decision artifact contract:
//
//   - changes_needed must point at concrete work: at least one
//     accepted item AND a non-empty round plan.md.
//   - clarification_needed must come paired with a non-empty
//     round clarification.md so the next code-review invocation
//     resumes this round instead of allocating a fresh one.
//   - no_changes_needed has no artifact requirement.
//
// Round carries the per-round paths so the caller does not have
// to thread them individually.
func ValidateRound(
	f ReviewFile, original SourceIDSet, round Round,
) error {
	if err := ValidatePost(f, original); err != nil {
		return err
	}
	switch f.Decision {
	case TopDecisionChangesNeeded:
		return validateChangesNeeded(f, round.PlanPath)
	case TopDecisionClarificationNeeded:
		return validateClarificationNeeded(round.ClarificationPath)
	}
	return nil
}

func validateChangesNeeded(f ReviewFile, planPath string) error {
	if !anyAccepted(f) {
		return errors.New(
			"codereview: changes_needed must mark at least one item " +
				"accepted; no accepted item means no work for the next " +
				"worker turn")
	}
	info, err := os.Stat(planPath)
	if err != nil {
		return fmt.Errorf(
			"codereview: round plan %q missing: %w", planPath, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf(
			"codereview: round plan %q is empty", planPath)
	}
	return nil
}

func validateClarificationNeeded(clarificationPath string) error {
	info, err := os.Stat(clarificationPath)
	if err != nil {
		return fmt.Errorf(
			"codereview: clarification_needed but %q missing: %w",
			clarificationPath, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf(
			"codereview: clarification_needed but %q is empty",
			clarificationPath)
	}
	return nil
}

func anyAccepted(f ReviewFile) bool {
	for _, it := range f.Items {
		if it.Decision == ItemDecisionAccepted {
			return true
		}
	}
	return false
}

func validateItem(it Item) error {
	if !AllowedItemDecisions[it.Decision] {
		return fmt.Errorf(
			"codereview: item %q invalid decision %q",
			it.SourceID, it.Decision)
	}
	if it.Decision == "" {
		return fmt.Errorf(
			"codereview: item %q missing decision", it.SourceID)
	}
	if it.Decision == ItemDecisionAccepted && it.PlanRef == "" {
		return fmt.Errorf(
			"codereview: accepted item %q missing plan_ref",
			it.SourceID)
	}
	if it.Reply != "" && utf8.RuneCountInString(it.Reply) > ReplyMaxRunes {
		return fmt.Errorf(
			"codereview: item %q reply exceeds %d runes",
			it.SourceID, ReplyMaxRunes)
	}
	return nil
}
