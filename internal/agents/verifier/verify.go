// Package verify implements the `j verify` subcommand. It resolves a
// work-done task to verify, prompts the user for a verifier agent /
// model, verifies that backend is signed in, and runs a bounded
// fix-loop alternating verifier turns with worker-resume turns until
// the verifier writes `VERDICT: PASS` to verifier_findings.md or the
// iteration cap is exhausted. The verifier writes verifier_plan.md
// and verifier_findings.md inside `<cwd>/.j/tasks/<id>/`; the worker
// edits project files in place when fixing findings.
package verifier

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// defaultMaxIterations bounds the verifier / worker fix loop.
const defaultMaxIterations = 3

// Options configures Run. Stdin/Stdout/Stderr default to the process
// streams. UI defaults to the huh implementation. Agents must be
// supplied by the caller (the CLI wires the Cursor agent; tests
// inject scripted ones). Interactive selects the agent's TUI when
// true and the headless path when false.
type Options struct {
	// TaskID, when set, names an existing task whose
	// `<cwd>/.j/tasks/<id>/` should be verified. The task row is
	// updated in place (work-done -> verifying ->
	// completed | failed | help).
	TaskID string
	// Yes, when true, skips the status-mismatch confirmation
	// prompt and proceeds even when the resolved task is not in
	// the work-done / failed / help allowlist. Mirrors the
	// `--yes` / VERIFY_YES flag wiring on the cobra command.
	Yes bool

	// Interactive is the resolved interactive flag. cobra cmd.go
	// computes it via resolver.Interactive before constructing Options.
	Interactive bool

	// Tool and Model are one-off overrides for the verifier
	// bucket's recorded tool/model. When either is set, Run resolves
	// the verifier via resolver.Agent (filling the missing half
	// from the bucket if needed) and skips persistence: the bucket
	// is left untouched. Mirrors the `j plan` / `j work` semantics.
	Tool  string
	Model string

	// MaxIterations bounds the verifier / worker-fix loop. Zero or
	// negative values fall back to defaultMaxIterations so callers
	// that build Options{} with a literal still get a sane bound.
	MaxIterations int

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	// Store, when non-nil, receives best-effort writes recording the
	// durable tool/model selection. The orchestrator does not own the
	// lifecycle when the caller supplies a Store. When nil, the helpers
	// below open `<cwd>/.j/settings` only for the duration of each
	// individual read/write so the bbolt file lock is never held across
	// the long-running agent.Verify invocation. Tests that supply a
	// Store directly skip the open/close cycle entirely.
	Store *store.Store
}

// Run executes `j verify`. Resolves the task, selects a
// verifier tool/model, then drives the bounded fix-loop until the
// verifier writes `VERDICT: PASS` or MaxIterations is exhausted.
// huh user-abort signals are translated to nil by CleanAbort.
func Run(ctx context.Context, opts Options) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("no coding agents configured")
	}

	res, ok, err := resolveTask(ctx, opts)
	if err != nil || !ok {
		return err
	}
	proceed, err := resolver.ConfirmStatusOverride(
		ctx, opts.UI, opts.Yes, "verify",
		res.Task, resolver.VerifyAllowed,
	)
	if err != nil || !proceed {
		return err
	}
	verifierAgent, session, err := resolveVerifyAgents(
		ctx, opts, res,
	)
	if err != nil {
		return err
	}

	lc := lifecycle.BeginVerifyRestart(
		res.Task, opts.Stderr, session,
	)
	outcome, runErr := runVerifyLoop(
		ctx, verifierAgent, lc, res, session, opts,
	)
	lc.Finish(outcome, runErr)
	if runErr != nil {
		return runErr
	}
	reportOutcome(opts.Stdout, outcome, res.Task.ID)
	return nil
}

// resolveVerifyAgents picks the verifier (bucket-resolved) and the
// worker (pinned to the row's WorkTool so the worker fix loop reuses
// the original session). NewResumeID is called eagerly: a failure
// warns to stderr but is non-fatal — the verifier still runs without
// a resume cursor on the first turn, matching prior behaviour.
func resolveVerifyAgents(
	ctx context.Context,
	opts Options,
	res resolver.VerifyTask,
) (codingagents.Agent, codingagents.AgentSession, error) {
	verifierAgent, model, err := selectVerifier(ctx, opts)
	if err != nil {
		return nil, codingagents.AgentSession{}, err
	}
	resumeID, err := verifierAgent.NewResumeID(ctx)
	if err != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", err)
	}
	if _, err := resolveFixAgent(opts.Agents, res.Task); err != nil {
		return nil, codingagents.AgentSession{}, err
	}
	return verifierAgent, codingagents.AgentSession{
		Tool:     verifierAgent.Name(),
		Model:    model,
		ResumeID: resumeID,
	}, nil
}

func reportOutcome(
	stdout io.Writer, outcome lifecycle.VerifyOutcome, taskID string,
) {
	switch outcome {
	case lifecycle.VerifyOutcomeSuccess:
		uitheme.NormalFprintf(
			stdout, "J: verified task %s\n", taskID,
		)
	case lifecycle.VerifyOutcomeNoRetries:
		uitheme.DangerousFprintf(
			stdout,
			"J: verifier exhausted retries on task %s; status failed\n",
			taskID,
		)
	}
}

func resolveTask(
	ctx context.Context,
	opts Options,
) (resolver.VerifyTask, bool, error) {
	return resolver.ResolveVerifyTask(ctx, resolver.VerifyTaskOptions{
		TaskID: opts.TaskID,
		UI:     opts.UI,
	})
}

func selectVerifier(
	ctx context.Context, opts Options,
) (codingagents.Agent, string, error) {
	return resolver.Agent(ctx, resolver.AgentOptions{
		Bucket:        store.BucketVerifier,
		Agents:        opts.Agents,
		ExplicitTool:  opts.Tool,
		ExplicitModel: opts.Model,
		UI:            opts.UI,
		Store:         opts.Store,
		Stderr:        opts.Stderr,
	})
}

func lookupResumeAgent(
	agents []codingagents.Agent, tool string,
) (codingagents.Agent, bool) {
	for _, agent := range agents {
		if agent.Name() == tool {
			return agent, true
		}
	}
	return nil, false
}

func resolveFixAgent(
	agents []codingagents.Agent,
	task tasks.Task,
) (codingagents.Agent, error) {
	workerAgent, ok := lookupResumeAgent(agents, task.WorkTool)
	if !ok {
		return nil, fmt.Errorf(
			"unknown tool %q (recorded on task %s)",
			task.WorkTool, task.ID,
		)
	}
	return workerAgent, nil
}

func (o Options) withDefaults() Options {
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
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	if o.MaxIterations <= 0 {
		o.MaxIterations = defaultMaxIterations
	}
	return o
}
