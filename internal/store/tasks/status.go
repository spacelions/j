package tasks

// TaskStatus is the typed string used in Task.Status. Only the values
// listed below are valid; Valid() is the allowlist guard used by
// PutTask so a misspelled enum can never reach disk.
type TaskStatus string

// Allowed Task.Status values. The set is intentionally closed: callers
// must use one of these constants. New states require a code change so
// the listing/sorting logic in `j tasks` can be updated together.
//
// `verifying`, `verify-done`, and `completed` are reserved for a
// future `j verify` command and are never written by `j plan` or
// `j work` today; the data model still includes them so listing/
// sorting code does not need to change when verification is wired up.
const (
	StatusPlanning   TaskStatus = "planning"
	StatusPlanDone   TaskStatus = "plan-done"
	StatusWorking    TaskStatus = "working"
	StatusWorkDone   TaskStatus = "work-done"
	StatusVerifying  TaskStatus = "verifying"
	StatusVerifyDone TaskStatus = "verify-done"
	StatusCompleted  TaskStatus = "completed"
	StatusHelp       TaskStatus = "help"
)

// Valid reports whether s is one of the allowlisted TaskStatus values.
func (s TaskStatus) Valid() bool {
	switch s {
	case StatusPlanning, StatusPlanDone, StatusWorking, StatusWorkDone,
		StatusVerifying, StatusVerifyDone, StatusCompleted, StatusHelp:
		return true
	}
	return false
}
