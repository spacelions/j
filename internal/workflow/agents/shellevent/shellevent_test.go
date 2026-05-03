package shellevent

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// TestYield_Success drives the success path: a non-error / non-
// escalate yield emits a textual event tagged with the supplied
// author and message.
func TestYield_Success(t *testing.T) {
	events := drainAgentWith(t, func(ctx agent.InvocationContext, yield func(*session.Event, error) bool) {
		Yield(ctx, yield, "planner", "planner phase complete", nil, false)
	})
	if len(events) == 0 {
		t.Fatalf("expected at least one event")
	}
	last := events[len(events)-1]
	if last.Author != "planner" {
		t.Fatalf("Author = %q, want planner", last.Author)
	}
	if last.LLMResponse.Content == nil || len(last.LLMResponse.Content.Parts) == 0 {
		t.Fatalf("event content empty")
	}
	if got := last.LLMResponse.Content.Parts[0].Text; got != "planner phase complete" {
		t.Fatalf("event text = %q", got)
	}
	if last.Actions.Escalate {
		t.Fatalf("Escalate must be false on a non-escalating success yield")
	}
}

// TestYield_SuccessEscalate drives the escalate=true branch.
func TestYield_SuccessEscalate(t *testing.T) {
	events := drainAgentWith(t, func(ctx agent.InvocationContext, yield func(*session.Event, error) bool) {
		Yield(ctx, yield, "verifier", "verifier phase complete (verdict=PASS)", nil, true)
	})
	var saw bool
	for _, ev := range events {
		if ev.Actions.Escalate {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected an event with Escalate=true; got %v", events)
	}
}

// TestYield_Error drives the error short-circuit: a non-nil err
// produces a (nil, wrapped) yield that includes the author prefix
// so a SequentialAgent / LoopAgent surfaces the failing phase by
// name.
func TestYield_Error(t *testing.T) {
	gotErr := drainAgentForErrorWith(t, func(ctx agent.InvocationContext, yield func(*session.Event, error) bool) {
		Yield(ctx, yield, "worker", "ignored", errors.New("worker boom"), false)
	})
	if gotErr == nil {
		t.Fatalf("expected error from Yield(error)")
	}
	if !strings.Contains(gotErr.Error(), "worker") || !strings.Contains(gotErr.Error(), "worker boom") {
		t.Fatalf("err = %v, want author prefix + cause", gotErr)
	}
}

// drainAgentWith builds a custom agent.Agent whose Run delegates to
// the supplied closure, runs it through a fresh runner.Run session,
// and returns every emitted event. A non-nil iterator error fails
// fatally; tests that expect an error use drainAgentForErrorWith.
func drainAgentWith(t *testing.T, body func(agent.InvocationContext, func(*session.Event, error) bool)) []*session.Event {
	t.Helper()
	a, err := agent.New(agent.Config{
		Name:        "test",
		Description: "shellevent yield test",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) { body(ctx, yield) }
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	ctx := context.Background()
	svc := session.InMemoryService()
	created, err := svc.Create(ctx, &session.CreateRequest{AppName: "t", UserID: "u"})
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	r, err := runner.New(runner.Config{AppName: "t", Agent: a, SessionService: svc})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}
	msg := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: ""}}}
	var events []*session.Event
	for ev, runErr := range r.Run(ctx, "u", created.Session.ID(), msg, agent.RunConfig{}) {
		if runErr != nil {
			t.Fatalf("runner err: %v", runErr)
		}
		events = append(events, ev)
	}
	return events
}

// drainAgentForErrorWith mirrors drainAgentWith but returns the
// first non-nil iterator error instead of failing on it.
func drainAgentForErrorWith(t *testing.T, body func(agent.InvocationContext, func(*session.Event, error) bool)) error {
	t.Helper()
	a, err := agent.New(agent.Config{
		Name:        "test",
		Description: "shellevent yield error test",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) { body(ctx, yield) }
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	ctx := context.Background()
	svc := session.InMemoryService()
	created, err := svc.Create(ctx, &session.CreateRequest{AppName: "t", UserID: "u"})
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	r, err := runner.New(runner.Config{AppName: "t", Agent: a, SessionService: svc})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}
	msg := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: ""}}}
	for _, runErr := range r.Run(ctx, "u", created.Session.ID(), msg, agent.RunConfig{}) {
		if runErr != nil {
			return runErr
		}
	}
	return nil
}
