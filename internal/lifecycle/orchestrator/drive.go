package orchestrator

import (
	"context"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// driveSequential constructs the smallest viable runner.New session
// the SequentialAgent can run inside and drains the resulting event
// iterator. The first error short-circuits.
//
// The user message is empty: the shell-out custom agents read
// everything they need from disk (per-task <id>/requirements.md /
// plan.md / verifier_findings.md); the orchestrator does not have
// to push a textual prompt.
func driveSequential(ctx context.Context, root agent.Agent) error {
	svc := session.InMemoryService()
	// InMemoryService.Create never fails; ignore the error.
	created, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: orchestratorAppName,
		UserID:  orchestratorUserID,
	})
	// runner.New only fails for nil Agent; ignore the error here.
	r, _ := runner.New(runner.Config{
		AppName:        orchestratorAppName,
		Agent:          root,
		SessionService: svc,
	})
	msg := &genai.Content{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: ""}},
	}
	for event, runErr := range r.Run(
		ctx, orchestratorUserID, created.Session.ID(),
		msg, agent.RunConfig{},
	) {
		if runErr != nil {
			return runErr
		}
		_ = event
	}
	return nil
}
