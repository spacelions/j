// Package workflow wires the Google ADK launcher and a planner/coder/verifier
// workflow: a SequentialAgent that first runs a planner, then a LoopAgent that
// iterates an inner SequentialAgent of coder -> verifier up to maxIterations.
//
// v1 has no ADK tools on any sub-agent. Because an LLM sub-agent without tools
// cannot easily set ctx.Actions().Escalate, the loop exits only when the
// configured MaxIterations is reached (see
// google.golang.org/adk/agent/workflowagents/loopagent). A future change can
// either add a tiny approve/reject function tool to the verifier, or attach an
// AfterAgentCallback that parses the verifier output and sets Escalate.
package workflow

import (
	"context"
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"

	"github.com/spacelions/j/internal/config"
	"github.com/spacelions/j/internal/workflow/agents/coder"
	"github.com/spacelions/j/internal/workflow/agents/planner"
	"github.com/spacelions/j/internal/workflow/agents/verifier"
)

// Run builds the planner/coder/verifier workflow and executes the ADK
// universal launcher. The cfg carries the runtime knobs (API key, model,
// iterations); launcherArgs are passed straight to the launcher parser
// (nil/empty for console, or "web" "api" "webui" for the local web stack).
func Run(ctx context.Context, cfg config.Config, launcherArgs []string) error {
	m, err := gemini.NewModel(ctx, cfg.Model, &genai.ClientConfig{APIKey: cfg.APIKey})
	if err != nil {
		return fmt.Errorf("workflow: model: %w", err)
	}

	p, err := planner.New(m)
	if err != nil {
		return fmt.Errorf("workflow: planner: %w", err)
	}

	c, err := coder.New(m)
	if err != nil {
		return fmt.Errorf("workflow: coder: %w", err)
	}

	vfr, err := verifier.New(m)
	if err != nil {
		return fmt.Errorf("workflow: verifier: %w", err)
	}

	innerBody, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "code_verify_body",
			Description: "Single coder -> verifier pass; one iteration of the outer loop.",
			SubAgents:   []agent.Agent{c, vfr},
		},
	})
	if err != nil {
		return fmt.Errorf("workflow: inner body: %w", err)
	}

	loop, err := loopagent.New(loopagent.Config{
		MaxIterations: cfg.MaxIterations,
		AgentConfig: agent.Config{
			Name:        "code_verify_loop",
			Description: "Iterates coder -> verifier up to a fixed number of passes.",
			SubAgents:   []agent.Agent{innerBody},
		},
	})
	if err != nil {
		return fmt.Errorf("workflow: loop: %w", err)
	}

	root, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "planner_coder_verifier",
			Description: "Runs the planner once, then loops coder -> verifier.",
			SubAgents:   []agent.Agent{p, loop},
		},
	})
	if err != nil {
		return fmt.Errorf("workflow: root: %w", err)
	}

	if err := full.NewLauncher().Execute(ctx, &launcher.Config{AgentLoader: agent.NewSingleLoader(root)}, launcherArgs); err != nil {
		return fmt.Errorf("workflow: %w", err)
	}
	return nil
}
