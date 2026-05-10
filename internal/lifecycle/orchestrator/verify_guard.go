package orchestrator

import (
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"

	"github.com/spacelions/j/internal/store/tasks"
)

// skipVerifyUnlessWorkDone wraps the verifier sub-agent so that the
// verifier only runs after the worker persisted `work-done`. A worker
// that stops at `needs-clarification`, `help`, or any other non-done
// state does not continue the same orchestrator invocation.
func skipVerifyUnlessWorkDone(
	taskID string, inner agent.Agent,
) (agent.Agent, error) {
	return agent.New(agent.Config{
		Name:        "work_verify_guard",
		Description: "Runs the verifier only after work-done.",
		SubAgents:   []agent.Agent{inner},
		Run:         guardRun(taskID, inner),
	})
}

func guardRun(
	taskID string, inner agent.Agent,
) func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(
		ctx agent.InvocationContext,
	) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			if rowIsNotWorkDone(taskID) {
				return
			}
			for ev, err := range inner.Run(ctx) {
				if !yield(ev, err) {
					return
				}
			}
		}
	}
}

// rowIsNotWorkDone reads the persisted row and reports whether it is
// anything other than `work-done`. Read errors count as "no" so the
// verifier runs on best-effort, matching the historical default when
// the row cannot be loaded.
func rowIsNotWorkDone(taskID string) bool {
	s, err := tasks.OpenDefault()
	if err != nil {
		return false
	}
	defer func() { _ = s.Close() }()
	t, err := s.GetTask(taskID)
	if err != nil {
		return false
	}
	return t.Status != tasks.StatusWorkDone
}
