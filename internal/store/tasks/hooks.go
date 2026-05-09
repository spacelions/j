package tasks

import (
	"sync"
	"testing"
)

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

// ResetHooksForTest clears the global hook registry. Panics outside
// of testing; test packages should defer it with t.Cleanup.
func ResetHooksForTest() {
	if !testing.Testing() {
		panic("ResetHooksForTest called outside of testing")
	}
	hooksMu.Lock()
	defer hooksMu.Unlock()
	hooks = nil
}
