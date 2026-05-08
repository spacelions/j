package tasks

// TaskStatus is the typed string used in Task.Status. Only the values
// listed below are valid; Valid() is the allowlist guard used by
// PutTask so a misspelled enum can never reach disk.
type TaskStatus string

// Allowed Task.Status values. The set is intentionally closed: callers
// must use one of these constants. New states require a code change so
// the listing/sorting logic in `j tasks` can be updated together.
//
// `completed` and `failed` are the two terminal outcomes of `j verify`:
// PASS finalises the task as `completed`, while a fix-loop that
// exhausts retries finalises as `failed`. `help` covers runtime
// errors during verify.
//
// `plan-pending-approval` is the plan gate: the planner finished but
// the user hasn't approved yet. `continue` on this row fires approval
// and proceeds to work.
//
// `needs-clarification` means the background agent wrote a question to
// `clarification.md` and is waiting for the user to answer.
const (
	StatusPlanning            TaskStatus = "planning"
	StatusPlanPendingApproval TaskStatus = "plan-pending-approval"
	StatusPlanDone            TaskStatus = "plan-done"
	StatusWorking             TaskStatus = "working"
	StatusWorkDone            TaskStatus = "work-done"
	StatusVerifying           TaskStatus = "verifying"
	StatusNeedsClarification  TaskStatus = "needs-clarification"
	StatusCompleted           TaskStatus = "completed"
	StatusFailed              TaskStatus = "failed"
	StatusHelp                TaskStatus = "help"
)

// Valid reports whether s is one of the allowlisted TaskStatus values.
func (s TaskStatus) Valid() bool {
	switch s {
	case StatusPlanning, StatusPlanPendingApproval, StatusPlanDone,
		StatusWorking, StatusWorkDone, StatusVerifying,
		StatusNeedsClarification, StatusCompleted, StatusFailed,
		StatusHelp:
		return true
	}
	return false
}
