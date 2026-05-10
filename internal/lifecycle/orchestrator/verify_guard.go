package orchestrator

import (
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"

	"github.com/spacelions/j/internal/store/tasks"
)

// skipVerifyOnClarification wraps the verifier sub-agent so that when
// the worker just halted at `needs-clarification` (foreground or
// reaper path), the verifier does NOT run on the same orchestrator
// invocation. The wrapper is a tiny custom agent that reads the row
// status and either short-circuits to a no-op event stream or forwards
// the inner verifier's events verbatim.
func skipVerifyOnClarification(
	taskID string,
	tagger func(string),
	inner agent.Agent,
) (agent.Agent, error) {
	return agent.New(agent.Config{
		Name: "work_verify_guard",
		Description: "Runs the verifier unless the worker stopped " +
			"at needs-clarification.",
		SubAgents: []agent.Agent{inner},
		Run:       guardRun(taskID, tagger, inner),
	})
}

func guardRun(
	taskID string,
	tagger func(string),
	inner agent.Agent,
) func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(
		ctx agent.InvocationContext,
	) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			if rowStoppedAtClarification(taskID) {
				return
			}
			if tagger != nil {
				tagger("verifying")
				if !yield(phaseEvent(ctx, "verifying"), nil) {
					return
				}
			}
			for ev, err := range inner.Run(ctx) {
				if !yield(ev, err) {
					return
				}
			}
		}
	}
}

// rowStoppedAtClarification reads the persisted row and reports
// whether it sits at `needs-clarification`. Read errors count as
// "no" so the verifier runs on best-effort — matching the
// historical default when the row cannot be loaded.
func rowStoppedAtClarification(taskID string) bool {
	s, err := tasks.OpenDefault()
	if err != nil {
		return false
	}
	defer func() { _ = s.Close() }()
	t, err := s.GetTask(taskID)
	if err != nil {
		return false
	}
	return t.Status == tasks.StatusNeedsClarification
}
