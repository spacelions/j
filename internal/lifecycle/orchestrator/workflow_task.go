package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"

	"github.com/spacelions/j/internal/agents/planner"
	"github.com/spacelions/j/internal/agents/verifier"
	"github.com/spacelions/j/internal/agents/worker"
)

// orchestratorAppName / orchestratorUserID name the synthetic
// runner.Run session the SequentialAgent runs inside. The IDs are
// internal to the orchestrator; nothing user-visible depends on
// them, but pinning them here keeps the runner.New / Create call
// pair self-consistent.
const (
	orchestratorAppName = "j-tasks-orchestrate"
	orchestratorUserID  = "j-tasks"
)

// PhaseOverrides carries one-off flag overrides for whichever phase
// the orchestrator is going to run. Zero value = no override (existing
// callers pass zero struct).
//
// Tool / Model / Yes are planner-specific; Interactive flows into the
// active phase (planner when not skipped, otherwise worker). Resume
// state is intentionally not part of this struct: the worker / verifier
// infer it from the task row's WorkResumeSession / VerifyResumeSession
// fields (re-work / re-verify clear them; resume-work / resume-verify
// leave them populated).
type PhaseOverrides struct {
	Tool        string
	Model       string
	Interactive bool
	Yes         bool
}

// RunPhase selects the slice of the planner→worker→verifier chain a
// single RunForTask invocation drives. Encoded as a string so it
// round-trips cleanly through cobra (`--phase=...`) / viper / agent
// log markers; expressing the previous bool-pair encoding's
// impossible combination is unrepresentable.
type RunPhase string

const (
	// RunPhaseFull runs planner → worker → verifier. Used by
	// `j tasks start` and `j tasks continue` on a fresh row.
	RunPhaseFull RunPhase = "full"
	// RunPhaseFromWork skips the planner and runs worker → verifier.
	// Used by `j tasks continue` on a plan-done row plus the re-work
	// / resume-work CLI wrappers.
	RunPhaseFromWork RunPhase = "from-work"
	// RunPhaseVerifyOnly runs only the verifier. Used by re-verify
	// / resume-verify.
	RunPhaseVerifyOnly RunPhase = "verify-only"
)

// ParseRunPhase resolves a string to a RunPhase. Empty maps to
// RunPhaseFull so a missing flag value behaves like the default. Any
// other unknown value is rejected so a typo at the CLI surfaces
// instead of silently running the planner.
func ParseRunPhase(s string) (RunPhase, error) {
	switch s {
	case "", string(RunPhaseFull):
		return RunPhaseFull, nil
	case string(RunPhaseFromWork):
		return RunPhaseFromWork, nil
	case string(RunPhaseVerifyOnly):
		return RunPhaseVerifyOnly, nil
	}
	return "", fmt.Errorf("workflow: unknown run phase %q (want full|from-work|verify-only)", s)
}

// RunForTask drives the planner → worker → verifier flow for an
// already-seeded task end to end.
func RunForTask(ctx context.Context, cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer, overrides PhaseOverrides) error {
	return runForTask(ctx, cfg, taskID, agents, stderr, RunPhaseFull, false, overrides)
}

// RunForTaskWithGate drives an already-seeded task, stopping after the
// planner when planRequiresApproval is true. A gated run leaves the
// row at plan-done so `j tasks continue --from-task <id>` can pick up
// the existing dispatch path.
func RunForTaskWithGate(ctx context.Context, cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer, planRequiresApproval bool, overrides PhaseOverrides) error {
	return runForTask(ctx, cfg, taskID, agents, stderr, RunPhaseFull, planRequiresApproval, overrides)
}

// RunForTaskFromWork drives an already-seeded task that is past the
// planner, running only worker → verifier. overrides.Interactive flows
// into the worker so `j tasks resume-work` / `re-work
// --interactive=true` surface the agent's TUI; the worker reads
// resume state from the task row's WorkResumeSession field directly
// (re-work clears it; resume-work leaves it).
func RunForTaskFromWork(ctx context.Context, cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer, overrides PhaseOverrides) error {
	return runForTask(ctx, cfg, taskID, agents, stderr, RunPhaseFromWork, false, overrides)
}

// RunForTaskVerifyOnly drives only the verifier phase on an
// already-seeded task. The verifier inspects the row's
// VerifyResumeSession to decide between Run / RunResume internally so
// the orchestrator does not need to thread interactive / resume
// through here.
func RunForTaskVerifyOnly(ctx context.Context, cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer) error {
	return runForTask(ctx, cfg, taskID, agents, stderr, RunPhaseVerifyOnly, false, PhaseOverrides{})
}

// runForTask builds a top-level SequentialAgent over shell-out custom
// agents constructed
// directly from each agents/{planner,worker,verifier} package's
// New(Config{...}) shell-out branch. The verifier flips Escalate on
// `VERDICT: PASS` so the structure stays compatible with a future
// enclosing LoopAgent without code changes.
//
// `j verify`'s internal worker→verifier fix loop already handles
// the retry semantics inside the verifier shell-agent (with
// MaxIterations=cfg.MaxIterations). Wrapping it in another
// LoopAgent would double-count iterations, so the top-level shape
// is intentionally a flat Sequential.
//
// stderr is wired into every shell-agent so any best-effort
// warnings from plan / work / verify lifecycles join the per-task
// agent.log.
//
// Lifecycle finalisation: after the SequentialAgent iterator
// drains, the per-task row is read and — if the verifier left it
// in `verifying` — flipped to `failed` so `j tasks` reflects
// the terminal outcome without waiting for the reaper.
//
// MaxIterations defaults are owned by callers: production callers
// fetch a sane default via store.LoadTaskConfig; tests that pass a
// zero-value Config flow through verifier.New's own fallback.
func runForTask(ctx context.Context, cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer, phase RunPhase, planRequiresApproval bool, overrides PhaseOverrides) error {
	if taskID == "" {
		return errors.New("workflow: task id required")
	}
	if len(agents) == 0 {
		return errors.New("workflow: no coding agents configured")
	}
	if stderr == nil {
		stderr = io.Discard
	}

	subAgents, err := taskSubAgents(cfg, taskID, agents, stderr, phase, planRequiresApproval, overrides)
	if err != nil {
		return err
	}

	root, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "planner_worker_verifier_task",
			Description: "Drives planner → worker → verifier for a single seeded task.",
			SubAgents:   subAgents,
		},
	})
	if err != nil {
		return fmt.Errorf("workflow: root: %w", err)
	}

	if err := driveSequential(ctx, root); err != nil {
		return err
	}
	finaliseVerifyFailIfStuck(stderr, taskID)
	return nil
}

func taskSubAgents(cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer, phase RunPhase, planRequiresApproval bool, overrides PhaseOverrides) ([]agent.Agent, error) {
	switch phase {
	case RunPhaseVerifyOnly:
		// The verifier internally decides between Run / RunResume by
		// inspecting the task's VerifyResumeSession (see
		// verifier.New), so the orchestrator does not have to thread
		// resume / interactive in here.
		verifierAgent, err := verifier.New(verifier.Config{
			TaskID:        taskID,
			Agents:        agents,
			Stderr:        stderr,
			MaxIterations: cfg.MaxIterations,
		})
		if err != nil {
			return nil, fmt.Errorf("workflow: verifier: %w", err)
		}
		return []agent.Agent{verifierAgent}, nil
	case RunPhaseFromWork:
		workerAgent, verifierAgent, err := newWorkVerify(cfg, taskID, agents, stderr, overrides.Interactive)
		if err != nil {
			return nil, err
		}
		return []agent.Agent{workerAgent, verifierAgent}, nil
	case RunPhaseFull:
		plannerAgent, err := planner.New(planner.Config{
			TaskID:      taskID,
			Agents:      agents,
			Stderr:      stderr,
			Tool:        overrides.Tool,
			Model:       overrides.Model,
			Interactive: overrides.Interactive,
			Yes:         overrides.Yes,
		})
		if err != nil {
			return nil, fmt.Errorf("workflow: planner: %w", err)
		}
		if planRequiresApproval {
			return []agent.Agent{plannerAgent}, nil
		}
		// The planner-then-worker handoff path leaves the worker
		// non-interactive: the planner's TUI exits cleanly as the
		// hand-off, and the worker proceeds headless.
		workerAgent, verifierAgent, err := newWorkVerify(cfg, taskID, agents, stderr, false)
		if err != nil {
			return nil, err
		}
		return []agent.Agent{plannerAgent, workerAgent, verifierAgent}, nil
	default:
		return nil, fmt.Errorf("workflow: unknown phase %q", phase)
	}
}

func newWorkVerify(cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer, workerInteractive bool) (agent.Agent, agent.Agent, error) {
	workerAgent, err := worker.New(worker.Config{
		TaskID:      taskID,
		Agents:      agents,
		Stderr:      stderr,
		Interactive: workerInteractive,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("workflow: worker: %w", err)
	}
	verifierAgent, err := verifier.New(verifier.Config{
		TaskID:        taskID,
		Agents:        agents,
		Stderr:        stderr,
		MaxIterations: cfg.MaxIterations,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("workflow: verifier: %w", err)
	}
	return workerAgent, verifierAgent, nil
}

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
	created, err := svc.Create(ctx, &session.CreateRequest{
		AppName: orchestratorAppName,
		UserID:  orchestratorUserID,
	})
	if err != nil {
		return fmt.Errorf("workflow: create session: %w", err)
	}
	r, err := runner.New(runner.Config{
		AppName:        orchestratorAppName,
		Agent:          root,
		SessionService: svc,
	})
	if err != nil {
		return fmt.Errorf("workflow: runner: %w", err)
	}
	msg := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: ""}}}
	for event, runErr := range r.Run(ctx, orchestratorUserID, created.Session.ID(), msg, agent.RunConfig{}) {
		if runErr != nil {
			return runErr
		}
		_ = event
	}
	return nil
}

