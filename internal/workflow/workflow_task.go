package workflow

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

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/workflow/agents/planner"
	"github.com/spacelions/j/internal/workflow/agents/verifier"
	"github.com/spacelions/j/internal/workflow/agents/worker"
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

// PlannerOverrides carries one-off flag overrides for the planner
// phase. Zero value = no override (existing callers pass zero struct).
type PlannerOverrides struct {
	Tool        string
	Model       string
	Interactive bool
	Yes         bool
}

// RunForTask drives the planner → worker → verifier flow for an
// already-seeded task end to end.
func RunForTask(ctx context.Context, cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer, overrides PlannerOverrides) error {
	return runForTask(ctx, cfg, taskID, agents, stderr, false, false, overrides)
}

// RunForTaskWithGate drives an already-seeded task, stopping after the
// planner when planRequiresApproval is true. A gated run leaves the
// row at plan-done so `j tasks continue --from-task <id>` can pick up
// the existing dispatch path.
func RunForTaskWithGate(ctx context.Context, cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer, planRequiresApproval bool, overrides PlannerOverrides) error {
	return runForTask(ctx, cfg, taskID, agents, stderr, planRequiresApproval, false, overrides)
}

// RunForTaskFromWork drives an already-seeded task that is past the
// planner, running only worker → verifier. Used by `j tasks continue`
// on a `plan-done` row so the implicit-approval handoff resumes the
// chain without re-running the planner.
func RunForTaskFromWork(ctx context.Context, cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer) error {
	return runForTask(ctx, cfg, taskID, agents, stderr, false, true, PlannerOverrides{})
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
// in `verifying` — flipped to `verify-done` so `j tasks` reflects
// the terminal outcome without waiting for the reaper.
//
// MaxIterations defaults are owned by callers: production callers
// fetch a sane default via store.LoadTaskConfig; tests that pass a
// zero-value Config flow through verifier.New's own fallback.
func runForTask(ctx context.Context, cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer, planRequiresApproval, skipPlanning bool, overrides PlannerOverrides) error {
	if taskID == "" {
		return errors.New("workflow: task id required")
	}
	if len(agents) == 0 {
		return errors.New("workflow: no coding agents configured")
	}
	if stderr == nil {
		stderr = io.Discard
	}

	subAgents, err := taskSubAgents(cfg, taskID, agents, stderr, planRequiresApproval, skipPlanning, overrides)
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

func taskSubAgents(cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer, planRequiresApproval, skipPlanning bool, overrides PlannerOverrides) ([]agent.Agent, error) {
	if planRequiresApproval && skipPlanning {
		return nil, errors.New("workflow: planRequiresApproval and skipPlanning are mutually exclusive")
	}
	if skipPlanning {
		workerAgent, verifierAgent, err := newWorkVerify(cfg, taskID, agents, stderr)
		if err != nil {
			return nil, err
		}
		return []agent.Agent{workerAgent, verifierAgent}, nil
	}
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
	workerAgent, verifierAgent, err := newWorkVerify(cfg, taskID, agents, stderr)
	if err != nil {
		return nil, err
	}
	return []agent.Agent{plannerAgent, workerAgent, verifierAgent}, nil
}

func newWorkVerify(cfg store.TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer) (agent.Agent, agent.Agent, error) {
	workerAgent, err := worker.New(worker.Config{
		TaskID: taskID,
		Agents: agents,
		Stderr: stderr,
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

// finaliseVerifyFailIfStuck flips a row still pinned at `verifying`
// to `verify-done` after the orchestrator's SequentialAgent drains.
// Best-effort: any read / write error surfaces as a single warning
// on stderr and the helper returns.
func finaliseVerifyFailIfStuck(stderr io.Writer, taskID string) {
	s, err := tasks.OpenDefault()
	if err != nil {
		uitheme.DangerousDialogBox(stderr, "J: tasks dir: %v", err)
		return
	}
	defer func() { _ = s.Close() }()
	t, err := s.GetTask(taskID)
	if err != nil {
		return
	}
	if t.Status != tasks.StatusVerifying {
		return
	}
	t.Status = tasks.StatusVerifyDone
	if err := s.PutTask(t); err != nil {
		uitheme.DangerousDialogBox(stderr, "J: tasks put: %v", err)
	}
}
