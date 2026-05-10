package orchestrator

import (
	"fmt"
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

type phaseAgent struct {
	phase string
	agent agent.Agent
}

func withPhaseTags(
	tagger func(string),
	items []phaseAgent,
) ([]agent.Agent, error) {
	out := make([]agent.Agent, 0, len(items)*2)
	for _, item := range items {
		if tagger != nil {
			a, err := newPhaseTagAgent(item.phase, tagger)
			if err != nil {
				return nil, err
			}
			out = append(out, a)
		}
		out = append(out, item.agent)
	}
	return out, nil
}

func newPhaseTagAgent(
	phase string,
	tagger func(string),
) (agent.Agent, error) {
	return agent.New(agent.Config{
		Name:        "phase_" + phase,
		Description: "Updates per-task lock phase metadata.",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				tagger(phase)
				yield(phaseEvent(ctx, phase), nil)
			}
		},
	})
}

func phaseEvent(ctx agent.InvocationContext, phase string) *session.Event {
	ev := session.NewEvent(ctx.InvocationID())
	ev.Author = "phase_" + phase
	ev.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Role: genai.RoleUser,
			Parts: []*genai.Part{{
				Text: fmt.Sprintf("phase %s started", phase),
			}},
		},
	}
	return ev
}
