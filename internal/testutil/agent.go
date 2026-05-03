package testutil

import (
	"context"
	"errors"
	"iter"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// DrainAgent runs the supplied agent through a fresh in-memory
// runner.Run session and returns every emitted event. A non-nil
// iterator error fails the test fatally; tests that want to assert
// the error use DrainAgentForError instead.
func DrainAgent(t *testing.T, a agent.Agent) []*session.Event {
	t.Helper()
	events, err := runAgent(t, a)
	if err != nil {
		t.Fatalf("testutil: runner err: %v", err)
	}
	return events
}

// DrainAgentForError mirrors DrainAgent but returns the first
// non-nil iterator error instead of failing on it. Returns nil if
// the iterator drains cleanly.
func DrainAgentForError(t *testing.T, a agent.Agent) error {
	t.Helper()
	_, err := runAgent(t, a)
	return err
}

// runAgent is the shared body behind DrainAgent / DrainAgentForError.
// It builds an in-memory session.Service, hands it to runner.New,
// and iterates the resulting event stream returning the first error
// it sees (or all events when none surface).
func runAgent(t *testing.T, a agent.Agent) ([]*session.Event, error) {
	t.Helper()
	ctx := context.Background()
	svc := session.InMemoryService()
	created, err := svc.Create(ctx, &session.CreateRequest{AppName: "t", UserID: "u"})
	if err != nil {
		t.Fatalf("testutil: session.Create: %v", err)
	}
	r, err := runner.New(runner.Config{AppName: "t", Agent: a, SessionService: svc})
	if err != nil {
		t.Fatalf("testutil: runner.New: %v", err)
	}
	msg := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: ""}}}
	var events []*session.Event
	for ev, runErr := range r.Run(ctx, "u", created.Session.ID(), msg, agent.RunConfig{}) {
		if runErr != nil {
			return events, runErr
		}
		events = append(events, ev)
	}
	return events, nil
}

// StubModel is a minimal model.LLM impl for the LLM-branch
// constructor tests. llmagent.New only consults Name() at
// construction time; GenerateContent is invoked at first Run,
// which the LLM-branch tests intentionally do not drive (no
// Gemini token in CI).
type StubModel struct{}

// Name returns a stable identifier llmagent.New accepts.
func (StubModel) Name() string { return "stub" }

// GenerateContent yields a single error so accidental drives of
// the LLM branch fail loudly instead of silently returning empty
// content.
func (StubModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(nil, errors.New("testutil.StubModel.GenerateContent should not be called"))
	}
}
