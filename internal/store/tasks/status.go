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
const (
	StatusPlanning  TaskStatus = "planning"
	StatusPlanDone  TaskStatus = "plan-done"
	StatusWorking   TaskStatus = "working"
	StatusWorkDone  TaskStatus = "work-done"
	StatusVerifying TaskStatus = "verifying"
	StatusFailed    TaskStatus = "failed"
	StatusCompleted TaskStatus = "completed"
	StatusHelp      TaskStatus = "help"
)

// Valid reports whether s is one of the allowlisted TaskStatus values.
func (s TaskStatus) Valid() bool {
	switch s {
	case StatusPlanning, StatusPlanDone, StatusWorking, StatusWorkDone,
		StatusVerifying, StatusFailed, StatusCompleted, StatusHelp:
		return true
	}
	return false
}
