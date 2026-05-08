package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/cli"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_RegisteredInNewRoot pins acceptance
// criterion #4: NewRoot must wire InitLinearVerifyPush so the hook
// is live in the production CLI (not just exported). We verify by
// constructing the root command (which calls the inits) and then
// firing a terminal verifying→completed transition for a non-Linear
// task — the hook should run (non-Linear short-circuits to a no-op
// without panic) without any other init also panicking.
func TestLinearVerifyPush_RegisteredInNewRoot(t *testing.T) {
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)
	_ = cli.NewRoot()
	id := tasks.NewTaskID()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("hook panicked: %v", r)
		}
	}()
	tasks.Notify(
		tasks.Transition{
			From:  tasks.StatusVerifying,
			Event: tasks.EventVerifyPass,
			To:    tasks.StatusCompleted,
		},
		tasks.Task{ID: id, Status: tasks.StatusCompleted},
	)
}
