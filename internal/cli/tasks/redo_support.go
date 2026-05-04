package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/plan"
	"github.com/spacelions/j/internal/cli/verify"
	"github.com/spacelions/j/internal/cli/work"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// redoPhase enumerates the per-phase re-entry commands wired under
// `j tasks plan|work|verify`. The dispatcher uses it to pick the
// resume-cursor field to inspect and the underlying phase Run /
// RunResume helpers to call.
type redoPhase string

const (
	redoPhasePlan   redoPhase = "plan"
	redoPhaseWork   redoPhase = "work"
	redoPhaseVerify redoPhase = "verify"
)

// RedoOptions configures runRedo. Stdin/Stdout/Stderr default to the
// process streams; UI defaults to the shared huh-backed task picker;
// Selector defaults to a huh-backed agent selector. Agents must be
// supplied by the caller (the cobra wiring injects the cursor +
// claude pair, tests inject scripted ones).
type RedoOptions struct {
	// TaskID is the optional `--from-task <id>` selector. When set
	// it skips the picker entirely and dispatches directly. An
	// empty value triggers the existing pickFromStore widget over
	// every task in the bbolt store.
	TaskID string

	// Interactive is the resolved per-call interactive flag. The
	// re-run branch forwards it into the underlying phase Options;
	// the resume branch ignores it (resume always runs interactive).
	Interactive bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI
	// Selector drives the agent-pick prompt(s) when
	// EnsureAgentSelections finds an empty bucket.
	Selector AgentSelector
}

// runRedoFns collects the underlying phase entry points used by
// runRedo. Tests assign scripted versions via t.Cleanup-restored
// swaps; production code never touches the package-level vars.
//
// Holding them in a package-level var (rather than introducing an
// exported seam) matches the existing pattern in this package: the
// continue dispatcher calls into the same phase packages directly
// and tests rely on per-package scripted agents to assert dispatch.
// runRedo, however, needs to test the resume-vs-rerun decision
// without actually invoking the agent, so we route through these
// vars and let tests substitute thin stubs.
var (
	runRedoPlanRun          = plan.Run
	runRedoPlanRunResume    = plan.RunResume
	runRedoWorkRun          = work.Run
	runRedoWorkRunResume    = work.RunResume
	runRedoVerifyRun        = verify.Run
	runRedoVerifyRunResume  = verify.RunResume
	runRedoResolveTask      = resolveContinueTask
	runRedoEnsureSelections = EnsureAgentSelections
)

// runRedo implements the shared dispatch for `j tasks plan`,
// `j tasks work`, and `j tasks verify`.
//
// Lifecycle:
//
//  1. Defer a huh.ErrUserAborted -> nil guard so a Ctrl-C in any
//     downstream prompt exits cleanly.
//  2. Resolve the target task: --from-task or pickFromStore (the
//     same picker `j tasks continue` uses). An empty store prints
//     `J: no tasks` and returns nil; a user-cancel in the picker
//     also returns nil.
//  3. Validate agent selections via EnsureAgentSelections so any
//     missing bucket prompts once before the dispatch fires.
//  4. Inspect the per-phase resume cursor on the row:
//     - non-empty → call the phase RunResume with TaskID set.
//     - empty     → call the phase Run with TaskID + the per-phase
//       Tool/Model from the row (empty falls through to the bucket
//       via resolver.Agent), Yes=true (the user explicitly chose
//       this phase), Interactive=opts.Interactive, Store=nil so
//       the bucket is never written by the explicit value.
func runRedo(ctx context.Context, phase redoPhase, opts RedoOptions) (err error) {
	defer func() {
		if errors.Is(err, huh.ErrUserAborted) {
			err = nil
		}
	}()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("J: no coding agents configured")
	}

	task, ok, err := runRedoResolveTask(ctx, ContinueOptions{
		TaskID: opts.TaskID,
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
		UI:     opts.UI,
	})
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if err := runRedoEnsureSelections(ctx, AgentCheckOptions{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
		Agents: opts.Agents,
		UI:     opts.Selector,
	}); err != nil {
		return err
	}

	return dispatchRedo(ctx, phase, opts, task)
}

// dispatchRedo picks the resume vs re-run branch for a single phase
// based on the per-phase resume cursor on task.
func dispatchRedo(ctx context.Context, phase redoPhase, opts RedoOptions, task store.Task) error {
	cursor, tool, model := redoPhaseFields(phase, task)
	switch phase {
	case redoPhasePlan:
		if cursor != "" {
			return runRedoPlanRunResume(ctx, plan.ResumeOptions{
				TaskID: task.ID,
				Stdin:  opts.Stdin,
				Stdout: opts.Stdout,
				Stderr: opts.Stderr,
				Agents: opts.Agents,
			})
		}
		return runRedoPlanRun(ctx, plan.Options{
			TaskID:      task.ID,
			Yes:         true,
			Interactive: opts.Interactive,
			Tool:        tool,
			Model:       model,
			Stdin:       opts.Stdin,
			Stdout:      opts.Stdout,
			Stderr:      opts.Stderr,
			Agents:      opts.Agents,
		})
	case redoPhaseWork:
		if cursor != "" {
			return runRedoWorkRunResume(ctx, work.ResumeOptions{
				TaskID: task.ID,
				Stdin:  opts.Stdin,
				Stdout: opts.Stdout,
				Stderr: opts.Stderr,
				Agents: opts.Agents,
			})
		}
		return runRedoWorkRun(ctx, work.Options{
			TaskID:      task.ID,
			Yes:         true,
			Interactive: opts.Interactive,
			Tool:        tool,
			Model:       model,
			Stdin:       opts.Stdin,
			Stdout:      opts.Stdout,
			Stderr:      opts.Stderr,
			Agents:      opts.Agents,
		})
	case redoPhaseVerify:
		if cursor != "" {
			return runRedoVerifyRunResume(ctx, verify.ResumeOptions{
				TaskID: task.ID,
				Stdin:  opts.Stdin,
				Stdout: opts.Stdout,
				Stderr: opts.Stderr,
				Agents: opts.Agents,
			})
		}
		return runRedoVerifyRun(ctx, verify.Options{
			TaskID:      task.ID,
			Yes:         true,
			Interactive: opts.Interactive,
			Tool:        tool,
			Model:       model,
			Stdin:       opts.Stdin,
			Stdout:      opts.Stdout,
			Stderr:      opts.Stderr,
			Agents:      opts.Agents,
		})
	}
	return fmt.Errorf("J: unsupported redo phase %q", phase)
}

// redoPhaseFields returns the per-phase resume cursor + per-phase
// tool/model for the supplied task. Empty values are returned
// verbatim so the re-run branch can pass them through to
// resolver.Agent (which falls back to the bucket and finally to a
// prompt).
func redoPhaseFields(phase redoPhase, task store.Task) (cursor, tool, model string) {
	switch phase {
	case redoPhasePlan:
		return task.PlanResumeCursor, task.PlanTool, task.PlanModel
	case redoPhaseWork:
		return task.WorkResumeCursor, task.WorkTool, task.WorkModel
	case redoPhaseVerify:
		return task.VerifyResumeCursor, task.VerifyTool, task.VerifyModel
	}
	return "", "", ""
}

func (o RedoOptions) withDefaults() RedoOptions {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.UI == nil {
		o.UI = newHuhUI(o.Stdin, o.Stderr)
	}
	if o.Selector == nil {
		o.Selector = picker.New(o.Stdin, o.Stderr)
	}
	return o
}
