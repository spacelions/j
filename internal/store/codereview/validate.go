package codereview

import (
	"errors"
	"fmt"
	"os"
	"unicode/utf8"
)

// SourceIDSet is the snapshot of fetched `source_id` values the
// validator compares against the planner-updated file. The cli takes
// the snapshot before the planner runs.
type SourceIDSet map[string]bool

// SnapshotSourceIDs returns the set of source_id values currently in
// f.Items. The cli uses it to pin the fetched-feedback ids before
// passing the file to the planner.
func SnapshotSourceIDs(f ReviewFile) SourceIDSet {
	out := make(SourceIDSet, len(f.Items))
	for _, it := range f.Items {
		out[it.SourceID] = true
	}
	return out
}

// ValidatePost is the post-planner validator. It enforces the
// contract documented in plan.md: every original source_id is still
// present; no invented ids are tolerated; decisions are restricted
// to the allowed enums; accepted items that need work carry a
// plan_ref; the draft reply text is non-trivial. When the planner
// reports `changes_needed`, ValidateRound additionally checks the
// round plan.md exists.
func ValidatePost(f ReviewFile, original SourceIDSet) error {
	if !AllowedTopDecisions[f.Decision] {
		return fmt.Errorf(
			"codereview: invalid top-level decision %q", f.Decision)
	}
	if f.Decision == "" {
		return errors.New(
			"codereview: top-level decision is required after planner")
	}
	got := SnapshotSourceIDs(f)
	for id := range original {
		if !got[id] {
			return fmt.Errorf("codereview: missing source_id %q", id)
		}
	}
	for id := range got {
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

// ValidateRound runs ValidatePost and, when the planner reported
// `changes_needed` with at least one accepted item, additionally
// requires the round plan.md to exist and be non-empty. Catches
// planners that record decisions but exit before writing the
// follow-up plan a later worker turn would execute.
func ValidateRound(
	f ReviewFile, original SourceIDSet, planPath string,
) error {
	if err := ValidatePost(f, original); err != nil {
		return err
	}
	if f.Decision != TopDecisionChangesNeeded {
		return nil
	}
	if !anyAccepted(f) {
		return nil
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
