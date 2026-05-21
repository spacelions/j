package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"

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

// RunForTask drives the planner → worker → verifier flow for an
// already-seeded task end to end.
func RunForTask(
	ctx context.Context, cfg store.TaskConfig, taskID string,
	agents []codingagents.Agent, stderr io.Writer,
	overrides PhaseOverrides,
) error {
	return RunForTaskWithGate(
		ctx,
		newTaskContext(cfg.MaxIterations, taskID, agents, stderr),
		PhaseConfig{Phase: RunPhaseFull, Overrides: overrides},
	)
}

// RunForTaskWithGate drives an already-seeded task, stopping after the
// planner when planRequiresApproval is true. A gated run leaves the
// row at plan-done so `j tasks continue --from-task <id>` can pick up
// the existing dispatch path.
func RunForTaskWithGate(
	ctx context.Context,
	tctx TaskContext,
	pc PhaseConfig,
) error {
	if pc.Phase == "" {
		pc.Phase = RunPhaseFull
	}
	return runForTask(ctx, tctx, pc)
}

// RunForTaskFromWork drives an already-seeded task that is past the
// planner, running only worker → verifier. overrides.Interactive flows
// into the worker so `j tasks resume-work` / `re-work
// --interactive=true` surface the agent's TUI; the worker reads
// resume state from the task row's WorkResumeSession field directly
// (re-work clears it; resume-work leaves it).
func RunForTaskFromWork(
	ctx context.Context, cfg store.TaskConfig, taskID string,
	agents []codingagents.Agent, stderr io.Writer,
	overrides PhaseOverrides,
) error {
	return runForTask(
		ctx,
		newTaskContext(cfg.MaxIterations, taskID, agents, stderr),
		PhaseConfig{Phase: RunPhaseFromWork, Overrides: overrides},
	)
}

func RunForTaskPlanOnly(
	ctx context.Context,
	tctx TaskContext,
	pc PhaseConfig,
) error {
	pc.Phase = RunPhasePlanOnly
	return runForTask(ctx, tctx, pc)
}

func RunForTaskWorkOnly(
	ctx context.Context,
	tctx TaskContext,
	pc PhaseConfig,
) error {
	pc.Phase = RunPhaseWorkOnly
	return runForTask(ctx, tctx, pc)
}

// RunForTaskVerifyOnly drives only the verifier phase on an
// already-seeded task. The verifier inspects the row's
// VerifyResumeSession to decide between Run / RunResume internally so
// the orchestrator does not need to thread interactive / resume
// through here.
func RunForTaskVerifyOnly(
	ctx context.Context, cfg store.TaskConfig, taskID string,
	agents []codingagents.Agent, stderr io.Writer,
) error {
	return runForTask(
		ctx,
		newTaskContext(cfg.MaxIterations, taskID, agents, stderr),
		PhaseConfig{Phase: RunPhaseVerifyOnly},
	)
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
func runForTask(
	ctx context.Context,
	tctx TaskContext,
	pc PhaseConfig,
) error {
	if tctx.TaskID == "" {
		return errors.New("workflow: task id required")
	}
	if len(tctx.Agents) == 0 {
		return errors.New("workflow: no coding agents configured")
	}
	if tctx.Stderr == nil {
		tctx.Stderr = io.Discard
	}

	subAgents, err := taskSubAgents(tctx, pc)
	if err != nil {
		return err
	}

	root, _ := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name: "planner_worker_verifier_task",
			Description: "Drives planner → worker → verifier for " +
				"a single seeded task.",
			SubAgents: subAgents,
		},
	})

	if err := driveSequential(ctx, root); err != nil {
		return err
	}
	finaliseVerifyFailIfStuck(tctx.Stderr, tctx.TaskID)
	return nil
}

func taskSubAgents(
	tctx TaskContext,
	pc PhaseConfig,
) ([]agent.Agent, error) {
	switch pc.Phase {
	case RunPhaseVerifyOnly:
		// The verifier internally decides between Run / RunResume by
		// inspecting the task's VerifyResumeSession (see
		// verifier.New), so the orchestrator does not have to thread
		// resume / interactive in here.
		verifierAgent, err := verifier.New(verifier.Config{
			TaskID:        tctx.TaskID,
			Agents:        tctx.Agents,
			Stderr:        tctx.Stderr,
			MaxIterations: tctx.MaxIterations,
		})
		if err != nil {
			return nil, fmt.Errorf("workflow: verifier: %w", err)
		}
		return withPhaseTags(pc.Tagger, []phaseAgent{
			{phase: "verifying", agent: verifierAgent},
		})
	case RunPhaseWorkOnly:
		workerAgent, err := newWorker(tctx, pc.Overrides.Interactive)
		if err != nil {
			return nil, err
		}
		return withPhaseTags(pc.Tagger, []phaseAgent{
			{phase: "working", agent: workerAgent},
		})
	case RunPhaseFromWork:
		workerAgent, guardedVerifier, err := newWorkVerify(
			tctx, pc.Overrides.Interactive, pc.Tagger)
		if err != nil {
			return nil, err
		}
		return withPhaseTagPrefix(
			pc.Tagger,
			[]phaseAgent{{phase: "working", agent: workerAgent}},
			guardedVerifier,
		), nil
	case RunPhasePlanOnly, RunPhaseFull:
		plannerAgent, err := planner.New(planner.Config{
			TaskID:      tctx.TaskID,
			Agents:      tctx.Agents,
			Stderr:      tctx.Stderr,
			Tool:        pc.Overrides.Tool,
			Model:       pc.Overrides.Model,
			Interactive: pc.Overrides.Interactive,
			Yes:         pc.Overrides.Yes,
		})
		if err != nil {
			return nil, fmt.Errorf("workflow: planner: %w", err)
		}
		if pc.Phase == RunPhasePlanOnly || pc.PlanRequiresApproval {
			return withPhaseTags(pc.Tagger, []phaseAgent{
				{phase: "planning", agent: plannerAgent},
			})
		}
		// The planner-then-worker handoff path leaves the worker
		// non-interactive: the planner's TUI exits cleanly as the
		// hand-off, and the worker proceeds headless.
		workerAgent, guardedVerifier, _ := newWorkVerify(
			tctx, false, pc.Tagger)
		return withPhaseTagPrefix(
			pc.Tagger,
			[]phaseAgent{
				{phase: "planning", agent: plannerAgent},
				{phase: "working", agent: workerAgent},
			},
			guardedVerifier,
		), nil
	default:
		return nil, fmt.Errorf("workflow: unknown phase %q", pc.Phase)
	}
}

func newWorkVerify(
	tctx TaskContext,
	workerInteractive bool,
	tagger func(string),
) (agent.Agent, agent.Agent, error) {
	workerAgent, err := newWorker(tctx, workerInteractive)
	if err != nil {
		return nil, nil, fmt.Errorf("workflow: worker: %w", err)
	}
	verifierAgent, _ := verifier.New(verifier.Config{
		TaskID:        tctx.TaskID,
		Agents:        tctx.Agents,
		Stderr:        tctx.Stderr,
		MaxIterations: tctx.MaxIterations,
	})
	guarded, _ := skipVerifyOnClarification(
		tctx.TaskID, tagger, verifierAgent)
	return workerAgent, guarded, nil
}

func newWorker(
	tctx TaskContext,
	interactive bool,
) (agent.Agent, error) {
	return worker.New(worker.Config{
		TaskID:      tctx.TaskID,
		Agents:      tctx.Agents,
		Stderr:      tctx.Stderr,
		Interactive: interactive,
	})
}
