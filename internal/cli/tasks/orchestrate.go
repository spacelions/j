package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle/orchestrator"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/agentlog"
)

// OrchestrateOptions configures RunOrchestrate. The detached child
// spawned by `j tasks start` re-invokes itself as
// `j tasks orchestrate --id <id>`; this struct lets tests drive the
// same entry point with stub coding agents.
type OrchestrateOptions struct {
	// TaskID names the seeded task whose planner → worker → verifier
	// chain this invocation drives end to end. Required.
	TaskID string

	// PlanRequiresApproval, when non-nil, is the resolved gate value
	// passed by `j tasks start`. nil makes direct/internal callers
	// inherit project.plan_requires_approval — but only on the
	// planner path; see Phase for the post-planner rule.
	PlanRequiresApproval *bool

	// Phase selects which slice of planner→worker→verifier runs.
	// RunPhaseFull (zero value) is the planner-led path and respects
	// PlanRequiresApproval. RunPhaseFromWork / RunPhaseVerifyOnly
	// short-circuit past the planner; the project default for
	// plan_requires_approval is intentionally ignored on those
	// paths so single-phase runs on opted-in projects do not hit
	// the conflict guard. The guard still fires on an *explicit*
	// PlanRequiresApproval=true paired with a non-Full phase.
	Phase orchestrator.RunPhase

	// Tool and Model are one-off planner overrides forwarded from
	// `j tasks start --tool/--model`.
	Tool  string
	Model string

	// Interactive controls whether the active phase (planner on
	// RunPhaseFull, worker on RunPhaseFromWork) runs in TUI mode.
	// Defaults to false (headless). Set by `j tasks start
	// --interactive` and the resume-* CLI wrappers.
	Interactive bool

	// Yes skips status-mismatch confirmation in the planner.
	Yes bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Agents is the wired coding-agent set; cobra wiring injects the
	// default backends while tests inject scripted ones.
	Agents []codingagents.Agent
}

// RunOrchestrate is the body of `j tasks orchestrate --id <id>`. It
// reads the relaxed per-project task config (`project.max_iterations`
// plus `project.plan_requires_approval` — `project.api_key` /
// `project.model` are NOT required on this path because the shell-out
// branch never instantiates a Gemini model), then dispatches by Phase
// to the matching orchestrator.RunForTask* entry point. The agent.log
// redirection is the parent's concern: the caller opens the per-task
// log with O_APPEND and passes its fd as our stdout/stderr, so any
// line the chain writes (including warnings from this function) lands
// chronologically.
func RunOrchestrate(ctx context.Context, opts OrchestrateOptions) error {
	opts = opts.withDefaults()
	if opts.TaskID == "" {
		return errors.New("tasks: orchestrate requires --id")
	}
	if len(opts.Agents) == 0 {
		return errors.New("tasks: no coding agents configured")
	}
	cfg, err := store.LoadTaskConfig()
	if err != nil {
		return err
	}
	ctx = tasks.WithPhase(ctx, phaseTagFor(opts.Phase))
	lock, err := acquireOrchestrateLock(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	ctx, stop := installOrchestrateSignalHandler(ctx)
	defer stop()
	emitSessionStart(opts.Stderr, opts.TaskID, opts.Phase)
	return dispatchOrchestratePhase(ctx, cfg, opts, lock)
}

// acquireOrchestrateLock takes the per-task flock for this orchestrate
// run. On contention the user-facing "task already in use" message is
// written to stderr before the *LockedError is bubbled up; non-locked
// failures pass through wrapped.
func acquireOrchestrateLock(
	ctx context.Context, opts OrchestrateOptions,
) (*tasks.Lock, error) {
	lock, err := tasks.AcquireLock(ctx, opts.TaskID)
	if err == nil {
		return lock, nil
	}
	var locked *tasks.LockedError
	if errors.As(err, &locked) {
		uitheme.DangerousDialogBox(opts.Stderr,
			"J: %s", contentionMessage(opts.TaskID, locked.Holder))
	}
	return nil, err
}

// dispatchOrchestratePhase fans out to the matching orchestrator entry
// point based on opts.Phase. Extracted from RunOrchestrate to keep the
// per-method cyclomatic complexity inside the linter budget.
func dispatchOrchestratePhase(
	ctx context.Context,
	cfg store.TaskConfig,
	opts OrchestrateOptions,
	lock *tasks.Lock,
) error {
	overrides := orchestrator.PhaseOverrides{
		Tool:        opts.Tool,
		Model:       opts.Model,
		Interactive: opts.Interactive,
		Yes:         opts.Yes,
	}
	tctx := orchestrator.TaskContext{
		MaxIterations: cfg.MaxIterations,
		TaskID:        opts.TaskID,
		Agents:        opts.Agents,
		Stderr:        opts.Stderr,
	}
	pc := orchestrator.PhaseConfig{
		Phase:     opts.Phase,
		Overrides: overrides,
		Tagger: func(phase string) {
			_ = lock.UpdatePhase(phase)
		},
	}
	if opts.Phase != orchestrator.RunPhaseFull && opts.Phase != "" {
		if explicitPlanApproval(opts) {
			return errPhaseConflictsWithApproval
		}
		return dispatchNonFullOrchestratePhase(ctx, tctx, pc)
	}
	return dispatchFullOrchestratePhase(ctx, opts, tctx, pc)
}

func explicitPlanApproval(opts OrchestrateOptions) bool {
	return opts.PlanRequiresApproval != nil && *opts.PlanRequiresApproval
}

func dispatchNonFullOrchestratePhase(
	ctx context.Context,
	tctx orchestrator.TaskContext,
	pc orchestrator.PhaseConfig,
) error {
	switch pc.Phase {
	case orchestrator.RunPhasePlanOnly:
		return orchestrator.RunForTaskPlanOnly(ctx, tctx, pc)
	case orchestrator.RunPhaseWorkOnly:
		return orchestrator.RunForTaskWorkOnly(ctx, tctx, pc)
	case orchestrator.RunPhaseVerifyOnly:
		pc.Phase = orchestrator.RunPhaseVerifyOnly
		return orchestrator.RunForTaskWithGate(ctx, tctx, pc)
	case orchestrator.RunPhaseFromWork:
		pc.Phase = orchestrator.RunPhaseFromWork
		return orchestrator.RunForTaskWithGate(ctx, tctx, pc)
	case orchestrator.RunPhaseFull, "":
		return fmt.Errorf("tasks: unknown phase %q", pc.Phase)
	}
	return fmt.Errorf("tasks: unknown phase %q", pc.Phase)
}

func dispatchFullOrchestratePhase(
	ctx context.Context,
	opts OrchestrateOptions,
	tctx orchestrator.TaskContext,
	pc orchestrator.PhaseConfig,
) error {
	approval, _ := resolvePlanRequiresApproval(
		opts.PlanRequiresApproval)
	pc.Phase = orchestrator.RunPhaseFull
	pc.PlanRequiresApproval = approval
	return orchestrator.RunForTaskWithGate(ctx, tctx, pc)
}

// errPhaseConflictsWithApproval is returned when a non-Full Phase is
// paired with an explicit PlanRequiresApproval=true override. The
// project default is intentionally ignored on non-Full phases, so the
// error fires only when the caller deliberately set the override.
var errPhaseConflictsWithApproval = errors.New(
	"tasks: non-full --phase (plan-only/from-work/work-only/verify-only) " +
		"is incompatible with " +
		flagPlanRequiresApprovalTrue)

// emitSessionStart writes one `session_start` marker into the agent
// log at the very top of orchestrate so a tailer can pin the task id,
// orchestrator pid, working directory, and selected phase without
// reading bbolt. Field collection is deliberately cheap — os.Hostname
// and os.Getwd never block — and write errors are swallowed because
// markers are observability signal, not load-bearing data.
func emitSessionStart(
	w io.Writer, taskID string, phase orchestrator.RunPhase,
) {
	hostname, _ := os.Hostname()
	cwd, _ := os.Getwd()
	if phase == "" {
		phase = orchestrator.RunPhaseFull
	}
	_ = agentlog.Emit(w, "session_start", map[string]any{
		"task":             taskID,
		"orchestrator_pid": os.Getpid(),
		"hostname":         hostname,
		"cwd":              cwd,
		"phase":            string(phase),
	})
}

func (o OrchestrateOptions) withDefaults() OrchestrateOptions {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	return o
}
