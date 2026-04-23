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
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

const (
	modelName           = "gemini-2.5-flash"
	maxIterations  uint = 3
)

// Run builds the planner/coder/verifier workflow and executes the ADK
// universal launcher. launcherArgs are passed straight to the launcher parser
// (nil/empty for console, or "web" "api" "webui" for the local web stack).
func Run(ctx context.Context, apiKey string, launcherArgs []string) error {
	model, err := gemini.NewModel(ctx, modelName, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return fmt.Errorf("workflow: model: %w", err)
	}

	planner, err := llmagent.New(llmagent.Config{
		Name:        "planner",
		Model:       model,
		Description: "Breaks the user's request into a concrete, ordered implementation plan.",
		Instruction: `You are the planner in a planner/coder/verifier workflow.
Read the user's request and produce a short, concrete plan that the coder can execute.
Rules:
- Focus on implementation steps, file boundaries, and acceptance criteria.
- Do not write code. Do not speculate about tools or infrastructure that is not requested.
- Keep it under ~15 numbered steps.
Output only the plan.`,
		OutputKey: "plan",
	})
	if err != nil {
		return fmt.Errorf("workflow: planner: %w", err)
	}

	coder, err := llmagent.New(llmagent.Config{
		Name:        "coder",
		Model:       model,
		Description: "Produces code from the plan, revising when verifier feedback is available.",
		Instruction: `You are the coder in a planner/coder/verifier workflow.

Plan:
{plan}

Latest verifier feedback (may be empty on the first iteration):
{temp:review?}

Task:
- Implement the plan as runnable code.
- If verifier feedback is present, revise the previous code to address it.
- Output only the final code, in a single fenced code block.`,
		OutputKey: "code",
	})
	if err != nil {
		return fmt.Errorf("workflow: coder: %w", err)
	}

	verifier, err := llmagent.New(llmagent.Config{
		Name:        "verifier",
		Model:       model,
		Description: "Reviews the coder's output against the plan and returns a concise verdict.",
		Instruction: `You are the verifier in a planner/coder/verifier workflow.

Plan:
{plan}

Code to review:
{code}

Task:
- Check the code against the plan: correctness, completeness, obvious bugs, and adherence to the acceptance criteria.
- Output a short bulleted review. End with a final line exactly one of:
  VERDICT: PASS
  VERDICT: FAIL`,
		OutputKey: "temp:review",
	})
	if err != nil {
		return fmt.Errorf("workflow: verifier: %w", err)
	}

	innerBody, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "code_verify_body",
			Description: "Single coder -> verifier pass; one iteration of the outer loop.",
			SubAgents:   []agent.Agent{coder, verifier},
		},
	})
	if err != nil {
		return fmt.Errorf("workflow: inner body: %w", err)
	}

	loop, err := loopagent.New(loopagent.Config{
		MaxIterations: maxIterations,
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
			SubAgents:   []agent.Agent{planner, loop},
		},
	})
	if err != nil {
		return fmt.Errorf("workflow: root: %w", err)
	}

	cfg := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(root),
	}

	l := full.NewLauncher()
	if err := l.Execute(ctx, cfg, launcherArgs); err != nil {
		return fmt.Errorf("workflow: %w", err)
	}
	return nil
}
