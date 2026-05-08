package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestVerify_StampTerminal_OnlyOnCompleted pins the centralised
// DoneAt-stamping policy that replaced the inline `if newStatus ==
// StatusCompleted { DoneAt = now }` branch in verify.Finish:
//   - StatusCompleted -> DoneAt must be stamped (non-zero).
//   - Every other status (including the terminal-but-failed
//     StatusFailed and the open phases planning / verifying / help)
//     -> DoneAt must be left untouched.
//
// The asymmetry matters: `j tasks` distinguishes "successful end"
// from "terminal failure" via DoneAt, so failed rows must NOT carry
// a stamp.
func TestVerify_StampTerminal_OnlyOnCompleted(t *testing.T) {
	cases := []struct {
		status tasks.TaskStatus
		stamp  bool
	}{
		{tasks.StatusCompleted, true},
		{tasks.StatusFailed, false},
		{tasks.StatusHelp, false},
		{tasks.StatusVerifying, false},
		{tasks.StatusPlanning, false},
		{tasks.StatusPlanDone, false},
		{tasks.StatusWorking, false},
		{tasks.StatusWorkDone, false},
		{tasks.StatusNeedsClarification, false},
		{tasks.StatusPlanPendingApproval, false},
	}
	for _, c := range cases {
		t.Run(string(c.status), func(t *testing.T) {
			row := tasks.Task{Status: c.status}
			tasks.StampTerminal(&row)
			if c.stamp && row.DoneAt.IsZero() {
				t.Fatalf("StampTerminal(%q) DoneAt zero, want stamped",
					c.status)
			}
			if !c.stamp && !row.DoneAt.IsZero() {
				t.Fatalf("StampTerminal(%q) stamped DoneAt; want zero",
					c.status)
			}
		})
	}
}
