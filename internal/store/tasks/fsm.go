// Package tasks provides the FSM governing legal TaskStatus
// transitions. Apply checks the transition table and returns the
// destination status (or an error). Callers persist the row and then
// call Notify to fire observer hooks with the durable snapshot. The
// table is the single source of truth; IsLegal / LegalEvents validate
// without mutating. The Mermaid diagram below mirrors the table.
//
//	stateDiagram-v2
//	    [*] --> planning : EventPlanBegin
//	    planning --> plan-done : EventPlanDone
//	    planning --> plan-done : EventReaperPlanDone
//	    planning --> plan-pending-approval : EventPlanAwaitApproval
//	    planning --> plan-pending-approval : EventReaperPlanAwaitApproval
//	    planning --> help : EventPlanError
//	    planning --> help : EventReaperPlanFail
//	    planning --> help : EventPlanQuit
//	    planning --> needs-clarification : EventReaperPlanNeedsClarification
//	    plan-pending-approval --> plan-done : EventPlanApprove
//	    plan-pending-approval --> planning : EventPlanRestart
//	    plan-pending-approval --> planning : EventPlanResume
//	    plan-done --> planning : EventPlanRestart
//	    plan-done --> planning : EventPlanResume
//	    plan-done --> working : EventWorkBegin
//	    plan-done --> working : EventWorkRestart
//	    working --> work-done : EventWorkDone
//	    working --> work-done : EventReaperWorkDone
//	    working --> help : EventWorkError
//	    working --> help : EventWorkQuit
//	    working --> needs-clarification : EventReaperWorkNeedsClarification
//	    working --> working : EventWorkResume
//	    work-done --> planning : EventPlanRestart
//	    work-done --> verifying : EventVerifyBegin
//	    work-done --> verifying : EventVerifyRestart
//	    work-done --> working : EventWorkRestart
//	    verifying --> completed : EventVerifyPass
//	    verifying --> failed : EventVerifyFail
//	    verifying --> failed : EventVerifyStuck
//	    verifying --> help : EventVerifyError
//	    verifying --> help : EventVerifyQuit
//	    verifying --> verifying : EventVerifyResume
//	    verifying --> needs-clarification : EventReaperVerifyNeedsClarification
//	    failed --> planning : EventPlanRestart
//	    failed --> planning : EventPlanResume
//	    failed --> verifying : EventVerifyRestart
//	    failed --> verifying : EventVerifyResume
//	    failed --> working : EventWorkRestart
//	    failed --> working : EventWorkResume
//	    completed --> planning : EventPlanRestart
//	    completed --> planning : EventPlanResume
//	    completed --> working : EventWorkRestart
//	    completed --> working : EventWorkResume
//	    completed --> verifying : EventVerifyRestart
//	    completed --> verifying : EventVerifyResume
//	    help --> planning : EventPlanRestart
//	    help --> planning : EventPlanResume
//	    help --> working : EventWorkRestart
//	    help --> working : EventWorkResume
//	    help --> verifying : EventVerifyRestart
//	    help --> verifying : EventVerifyResume
//	    needs-clarification --> planning : EventPlanResume
//	    needs-clarification --> working : EventWorkResume
//	    needs-clarification --> verifying : EventVerifyResume
//	    completed --> [*]
//	    failed --> [*]
//	    help --> [*]
package tasks

import (
	"fmt"
	"sync"
	"testing"
)

// Event is a typed string naming a lifecycle event. Events are
// cause-named (not destination-named) so EventPlanError describes
// *what happened* rather than *where to go*.
type Event string

const (
	EventPlanBegin                   Event = "plan_begin"
	EventPlanRestart                 Event = "plan_restart"
	EventPlanDone                    Event = "plan_done"
	EventPlanAwaitApproval           Event = "plan_await_approval"
	EventPlanApprove                 Event = "plan_approve"
	EventPlanQuit                    Event = "plan_quit"
	EventPlanError                   Event = "plan_error"
	EventPlanResume                  Event = "plan_resume"
	EventReaperPlanDone              Event = "reaper_plan_done"
	EventReaperPlanAwaitApproval     Event = "reaper_plan_await_approval"
	EventReaperPlanFail              Event = "reaper_plan_fail"
	EventReaperPlanNeedsClarification Event = "reaper_plan_needs_clarification"

	EventWorkBegin                   Event = "work_begin"
	EventWorkRestart                 Event = "work_restart"
	EventWorkResume                  Event = "work_resume"
	EventWorkDone                    Event = "work_done"
	EventWorkQuit                    Event = "work_quit"
	EventWorkError                   Event = "work_error"
	EventReaperWorkDone              Event = "reaper_work_done"
	EventReaperWorkNeedsClarification Event = "reaper_work_needs_clarification"

	EventVerifyBegin                 Event = "verify_begin"
	EventVerifyRestart               Event = "verify_restart"
	EventVerifyResume                Event = "verify_resume"
	EventVerifyPass                  Event = "verify_pass"
	EventVerifyFail                  Event = "verify_fail"
	EventVerifyQuit                  Event = "verify_quit"
	EventVerifyError                 Event = "verify_error"
	EventVerifyStuck                 Event = "verify_stuck"
	EventReaperVerifyNeedsClarification Event = "reaper_verify_needs_clarification"
)

// Transition records one legal FSM edge.
type Transition struct {
	From  TaskStatus
	Event Event
	To    TaskStatus
}

// transitions is the single source of truth for every legal edge.
// The zero TaskStatus "" represents a new task with no status yet.
var transitions = []Transition{
	{StatusPlanning, EventPlanDone, StatusPlanDone},
	{StatusPlanning, EventPlanAwaitApproval, StatusPlanPendingApproval},
	{StatusPlanning, EventPlanError, StatusHelp},
	{StatusPlanning, EventPlanQuit, StatusHelp},
	{StatusPlanning, EventReaperPlanDone, StatusPlanDone},
	{StatusPlanning, EventReaperPlanAwaitApproval, StatusPlanPendingApproval},
	{StatusPlanning, EventReaperPlanFail, StatusHelp},
	{StatusPlanning, EventReaperPlanNeedsClarification, StatusNeedsClarification},

	{StatusPlanning, EventPlanRestart, StatusPlanning},
	{StatusPlanning, EventWorkRestart, StatusWorking},
	{StatusPlanning, EventVerifyBegin, StatusVerifying},

	{StatusPlanPendingApproval, EventPlanApprove, StatusPlanDone},
	{StatusPlanPendingApproval, EventPlanRestart, StatusPlanning},
	{StatusPlanPendingApproval, EventPlanResume, StatusPlanning},

	{StatusPlanDone, EventPlanRestart, StatusPlanning},
	{StatusPlanDone, EventPlanResume, StatusPlanning},
	{StatusPlanDone, EventWorkBegin, StatusWorking},
	{StatusPlanDone, EventWorkRestart, StatusWorking},
	{StatusPlanDone, EventVerifyBegin, StatusVerifying},
	{StatusPlanDone, EventVerifyResume, StatusVerifying},

	{StatusWorking, EventPlanRestart, StatusPlanning},
	{StatusWorking, EventVerifyRestart, StatusVerifying},
	{StatusWorking, EventWorkRestart, StatusWorking},
	{StatusWorking, EventWorkDone, StatusWorkDone},
	{StatusWorking, EventWorkQuit, StatusHelp},
	{StatusWorking, EventWorkError, StatusHelp},
	{StatusWorking, EventWorkResume, StatusWorking},
	{StatusWorking, EventReaperWorkDone, StatusWorkDone},
	{StatusWorking, EventReaperWorkNeedsClarification, StatusNeedsClarification},

	{StatusWorkDone, EventPlanRestart, StatusPlanning},
	{StatusWorkDone, EventWorkRestart, StatusWorking},
	{StatusWorkDone, EventVerifyBegin, StatusVerifying},
	{StatusWorkDone, EventVerifyRestart, StatusVerifying},

	{StatusVerifying, EventVerifyBegin, StatusVerifying},
	{StatusVerifying, EventVerifyRestart, StatusVerifying},
	{StatusVerifying, EventVerifyPass, StatusCompleted},
	{StatusVerifying, EventVerifyFail, StatusFailed},
	{StatusVerifying, EventVerifyStuck, StatusFailed},
	{StatusVerifying, EventVerifyError, StatusHelp},
	{StatusVerifying, EventVerifyQuit, StatusHelp},
	{StatusVerifying, EventVerifyResume, StatusVerifying},
	{StatusVerifying, EventReaperVerifyNeedsClarification,
		StatusNeedsClarification},

	{StatusFailed, EventPlanRestart, StatusPlanning},
	{StatusFailed, EventWorkRestart, StatusWorking},
	{StatusFailed, EventVerifyRestart, StatusVerifying},
	{StatusFailed, EventPlanResume, StatusPlanning},
	{StatusFailed, EventWorkResume, StatusWorking},
	{StatusFailed, EventVerifyResume, StatusVerifying},
	{StatusCompleted, EventPlanRestart, StatusPlanning},
	{StatusCompleted, EventPlanResume, StatusPlanning},
	{StatusCompleted, EventWorkRestart, StatusWorking},
	{StatusCompleted, EventWorkResume, StatusWorking},
	{StatusCompleted, EventVerifyRestart, StatusVerifying},
	{StatusCompleted, EventVerifyResume, StatusVerifying},
	{StatusHelp, EventPlanRestart, StatusPlanning},
	{StatusHelp, EventPlanResume, StatusPlanning},
	{StatusHelp, EventWorkRestart, StatusWorking},
	{StatusHelp, EventWorkResume, StatusWorking},
	{StatusHelp, EventVerifyRestart, StatusVerifying},
	{StatusHelp, EventVerifyResume, StatusVerifying},
	{StatusNeedsClarification, EventPlanResume, StatusPlanning},
	{StatusNeedsClarification, EventWorkResume, StatusWorking},
	{StatusNeedsClarification, EventVerifyResume, StatusVerifying},
	{"", EventPlanBegin, StatusPlanning},
}

// IllegalTransitionError is returned by Apply when an event is not
// legal from the current status.
type IllegalTransitionError struct {
	From  TaskStatus
	Event Event
}

func (e IllegalTransitionError) Error() string {
	return fmt.Sprintf(
		"illegal transition: cannot %q from status %q", e.Event, e.From)
}

// Apply checks whether the edge (from, e) is legal. On success it
// returns the destination status. On failure it returns an
// IllegalTransitionError.
//
// Apply does NOT fire hooks. Callers should persist the row and then
// call Notify(Transition{from, e, to}, task) to fire observers with
// the durable task snapshot.
func Apply(from TaskStatus, e Event) (TaskStatus, error) {
	for _, tr := range transitions {
		if tr.From == from && tr.Event == e {
			return tr.To, nil
		}
	}
	return "", IllegalTransitionError{From: from, Event: e}
}

// IsLegal reports whether the edge (from, e) appears in the
// transition table. It does not fire hooks.
func IsLegal(from TaskStatus, e Event) bool {
	for _, tr := range transitions {
		if tr.From == from && tr.Event == e {
			return true
		}
	}
	return false
}

// LegalEvents returns every event that is legal from the given
// source status. A new task (zero value) is represented by "".
func LegalEvents(from TaskStatus) []Event {
	out := make([]Event, 0)
	for _, tr := range transitions {
		if tr.From == from {
			out = append(out, tr.Event)
		}
	}
	return out
}

// Hook observes a transition that has already been applied and
// persisted. Hooks MUST NOT mutate the task or call back into
// Apply; they are observers, not participants.
type Hook func(t Transition, task Task)

var (
	hooksMu sync.RWMutex
	hooks   []Hook
)

// Register adds a hook for the lifetime of the process. Hooks fire
// in registration order, synchronously, on the goroutine that called
// Notify. A panic in one hook does not skip the others.
func Register(h Hook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	hooks = append(hooks, h)
}

// Notify fires every registered Hook synchronously in registration
// order for the given transition and task snapshot. A panic in one
// hook is recovered and does not interrupt subsequent hooks.
//
// Callers should call Notify after persisting the row so hooks
// always observe durable state.
func Notify(tr Transition, task Task) {
	hooksMu.RLock()
	hs := make([]Hook, len(hooks))
	copy(hs, hooks)
	hooksMu.RUnlock()

	for _, h := range hs {
		func() {
			defer func() {
				_ = recover()
			}()
			h(tr, task)
		}()
	}
}

// ResetHooksForTest clears the global hook registry. It panics when
// called outside of testing so a production path cannot accidentally
// lose hooks. Test packages should defer it with t.Cleanup.
func ResetHooksForTest() {
	if !testing.Testing() {
		panic("ResetHooksForTest called outside of testing")
	}
	hooksMu.Lock()
	defer hooksMu.Unlock()
	hooks = nil
}
