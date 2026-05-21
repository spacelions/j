// Package orchestrator wires the Google ADK launcher and a
// planner/worker/verifier workflow: a SequentialAgent that first
// runs a planner, then a LoopAgent that iterates an inner
// SequentialAgent of worker -> verifier up to maxIterations.
//
// v1 has no ADK tools on any sub-agent. Because an LLM sub-agent
// without tools cannot easily set ctx.Actions().Escalate, the loop
// exits only when the configured MaxIterations is reached (see
// google.golang.org/adk/agent/workflowagents/loopagent). A future
// change can either add a tiny approve/reject function tool to the
// verifier, or attach an AfterAgentCallback that parses the verifier
// output and sets Escalate.
package orchestrator

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

	"github.com/spacelions/j/internal/agents/planner"
	"github.com/spacelions/j/internal/agents/verifier"
	"github.com/spacelions/j/internal/agents/worker"
	"github.com/spacelions/j/internal/store"
)

// Run builds the planner/worker/verifier workflow and executes the ADK
// universal launcher. The cfg carries the runtime knobs (API key, model,
// iterations); launcherArgs are passed straight to the launcher parser
// (nil/empty for console, or "web" "api" "webui" for the local web stack).
func Run(
	ctx context.Context,
	cfg store.ProjectConfig,
	launcherArgs []string,
) error {
	// All constructors below only fail on programmer error; ignore errors.
	m, _ := gemini.NewModel(
		ctx, cfg.Model, &genai.ClientConfig{APIKey: cfg.APIKey})
	p, _ := planner.New(planner.Config{LLM: m})
	w, _ := worker.New(worker.Config{LLM: m})
	vfr, _ := verifier.New(verifier.Config{LLM: m})
	innerBody, _ := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "code_verify_body",
			SubAgents: []agent.Agent{w, vfr},
		},
	})
	loop, _ := loopagent.New(loopagent.Config{
		MaxIterations: cfg.MaxIterations,
		AgentConfig: agent.Config{
			Name:      "code_verify_loop",
			SubAgents: []agent.Agent{innerBody},
		},
	})
	root, _ := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "planner_worker_verifier",
			SubAgents: []agent.Agent{p, loop},
		},
	})

	cfgL := &launcher.Config{AgentLoader: agent.NewSingleLoader(root)}
	if err := full.NewLauncher().Execute(ctx, cfgL, launcherArgs); err != nil {
		return fmt.Errorf("workflow: %w", err)
	}
	return nil
}
