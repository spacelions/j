package store

import (
	"sort"
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
	sort.SliceStable(tasks, func(i, j int) bool {
		ai, aj := taskIsActive(tasks[i].Status), taskIsActive(tasks[j].Status)
		if ai != aj {
			return ai
		}
		if ai {
			return tasks[i].ID < tasks[j].ID
		}
		ti, tj := taskFallbackTime(tasks[i]), taskFallbackTime(tasks[j])
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return tasks[i].ID > tasks[j].ID
	})
}

// taskIsActive returns true for the four "still in flight" statuses.
// Anything else (plan-done, work-done, verify-done, completed, plus
// any future inactive state) is treated as inactive by SortTasks.
func taskIsActive(s TaskStatus) bool {
	switch s {
	case StatusPlanning, StatusWorking, StatusVerifying, StatusHelp:
		return true
	}
	return false
}

// taskFallbackTime returns the timestamp SortTasks compares for
// inactive tasks. The cascade reflects how a task's lifecycle ends:
// completed tasks have done_at; verify-done tasks only have
// verify_end_at; work-done tasks only have work_end_at; plan-done
// tasks only have plan_end_at; anything older falls through to the
// zero time so ID order takes over.
func taskFallbackTime(t Task) time.Time {
	switch {
	case t.DoneAt != nil:
		return *t.DoneAt
	case t.VerifyEndAt != nil:
		return *t.VerifyEndAt
	case t.WorkEndAt != nil:
		return *t.WorkEndAt
	case t.PlanEndAt != nil:
		return *t.PlanEndAt
	}
	return time.Time{}
}
