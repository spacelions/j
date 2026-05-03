// Package shellevent factors out the per-phase summary event the
// planner / worker / verifier shell-out custom agents emit so all
// three converge on one helper. It depends only on the ADK session /
// model / agent surfaces — never on cli/* or coding-agents/* — so any
// agents/{planner,worker,verifier} package can import it without
// re-introducing the agents → cli → coding-agents → prompts → agents
// import cycle.
package shellevent

import (
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// Yield emits the standard per-phase summary event. err non-nil
// short-circuits to a (nil, err) yield so the parent SequentialAgent
// stops with the wrapped failure; on success it yields a textual
// event tagged with author and optionally flips Escalate so a future
// enclosing LoopAgent observes the PASS verdict and exits early.
//
// The yield-bool return is intentionally ignored because the helper
// is the last action in every shell-agent's Run closure; nothing
// downstream needs to know whether the consumer wants more events.
func Yield(ctx agent.InvocationContext, yield func(*session.Event, error) bool, author, msg string, err error, escalate bool) {
	if err != nil {
		yield(nil, fmt.Errorf("%s: %w", author, err))
		return
	}
	event := session.NewEvent(ctx.InvocationID())
	event.Author = author
	event.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Role:  genai.RoleUser,
			Parts: []*genai.Part{{Text: msg}},
		},
	}
	if escalate {
		event.Actions.Escalate = true
	}
	yield(event, nil)
}
