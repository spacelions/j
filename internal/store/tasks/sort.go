package tasks

import (
	"slices"
	"time"
)

// SortTasks orders tasks the way `j tasks` displays them:
//
//  1. Active states (planning, working, verifying, help) come first,
//     among themselves sorted by ID ascending. ID is time-sortable so
//     this is effectively "earliest started first" with a stable
//     tiebreak.
//  2. Inactive states (planned, done, plus any future non-active
//     status) come after, sorted by done_at descending. When done_at
//     is missing we fall back to work_end_at, then plan_end_at, then
//     finally to ID descending so newer-started entries float up.
//
// The function mutates tasks in place and returns nothing because the
// receiver convention here matches sort.Slice's existing call sites.
func SortTasks(tasks []Task) {
	slices.SortStableFunc(tasks, func(a, b Task) int {
		ai, bi := taskIsActive(a.Status), taskIsActive(b.Status)
		if ai != bi {
			if ai {
				return -1
			}
			return 1
		}
		if ai {
			if a.ID < b.ID {
				return -1
			}
			if a.ID > b.ID {
				return 1
			}
			return 0
		}
		ta, tb := taskFallbackTime(a), taskFallbackTime(b)
		if !ta.Equal(tb) {
			if ta.After(tb) {
				return -1
			}
			return 1
		}
		if a.ID > b.ID {
			return -1
		}
		if a.ID < b.ID {
			return 1
		}
		return 0
	})
}

// taskIsActive returns true for "still in flight" statuses.
// needs-clarification and plan-pending-approval are active:
// they represent a task blocked on user input.
func taskIsActive(s TaskStatus) bool {
	switch s {
	case StatusPlanning, StatusPlanPendingApproval, StatusWorking,
		StatusVerifying, StatusNeedsClarification, StatusHelp:
		return true
	default:
		return false
	}
}

// taskFallbackTime returns the timestamp SortTasks compares for
// inactive tasks. The cascade reflects how a task's lifecycle ends:
// completed tasks have done_at; failed tasks only have
// verify_end_at; work-done tasks only have work_end_at; plan-done
// tasks only have plan_end_at; anything older falls through to the
// zero time so ID order takes over.
func taskFallbackTime(t Task) time.Time {
	switch {
	case !t.DoneAt.IsZero():
		return t.DoneAt
	case !t.VerifyEndAt.IsZero():
		return t.VerifyEndAt
	case !t.WorkEndAt.IsZero():
		return t.WorkEndAt
	case !t.PlanEndAt.IsZero():
		return t.PlanEndAt
	}
	return time.Time{}
}
