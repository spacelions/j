package codereview

import (
	"errors"
	"fmt"
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
// plan_ref; the draft reply text is non-trivial.
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
	if it.Decision == "accepted" && it.PlanRef == "" {
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
