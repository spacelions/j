// Package verify implements the `j verify` subcommand. It resolves a
// work-done task to verify, prompts the user for a verifier agent /
// model, verifies that backend is signed in, and runs a bounded
// fix-loop alternating verifier turns with worker-resume turns until
// the verifier writes `VERDICT: PASS` to verifier_findings.md or the
// iteration cap is exhausted. The verifier writes verifier_plan.md
// and verifier_findings.md inside `<cwd>/.j/tasks/<id>/`; the worker
// edits project files in place when fixing findings.
package verify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/banner"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/run"
)

// Options configures Run. Stdin/Stdout/Stderr default to the process
// streams. UI defaults to the huh implementation. Agents must be
// supplied by the caller (the CLI wires the Cursor agent; tests
// inject scripted ones). Interactive selects the agent's TUI when
// true and the headless path when false.
type Options struct {
	// TaskID, when set, names an existing task whose
	// `<cwd>/.j/tasks/<id>/` should be verified. The task row is
	// updated in place (work-done -> verifying ->
	// completed | verify-done | help).
	TaskID string
	// Yes, when true, skips the status-mismatch confirmation
	// prompt and proceeds even when the resolved task is not in
	// the work-done / verify-done / help allowlist. Mirrors the
	// `--yes` / VERIFY_YES flag wiring on the cobra command.
	Yes bool

	// Interactive is the resolved interactive flag. cobra cmd.go
	// computes it via resolver.Interactive (explicit > stored > true)
	// before constructing Options.
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
	// tool/model/interactive flag last used. The orchestrator does
	// not own the lifecycle when the caller supplies a Store. When
	// nil, the helpers below open `<cwd>/.j/settings` only for the
	// duration of each individual read/write so the bbolt file lock
	// is never held across the long-running agent.Verify
	// invocation. Tests that supply a Store directly skip the
	// open/close cycle entirely.
	Store *store.Store
}

// resolved is the outcome of resolveTask: the existing bbolt row
// plus the per-task absolute paths the verifier consumes.
type resolved struct {
	Task             store.Task
	TaskDir          string
	RequirementsPath string
	PlanPath         string
	VerifierPlanPath string
	FindingsPath     string
}

// Run executes `j verify`. It resolves the task source, selects a
// verifier tool/model, then runs the bounded fix-loop until the
// verifier returns VERDICT: PASS or the loop is exhausted.
//
// User-abort signals from any huh prompt (Ctrl+C / Esc) propagate up
// as huh.ErrUserAborted; the deferred guard below converts them to a
// nil return so an explicit cancel exits the command cleanly without
// printing a bogus "cancelled by user" line.
//
// The bbolt file lock on `<cwd>/.j/settings` is never held across the
// agent.Verify / agent.Work calls: each settings read/write below
// opens the DB, performs the operation, and closes before any agent
// work begins.
func Run(ctx context.Context, opts Options) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("J: no coding agents configured")
	}

	res, ok, err := resolveTask(ctx, opts)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	proceed, err := confirmStatusOverride(ctx, opts, "verify", res.Task, allowedForVerify)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	verifierAgent, model, err := selectVerifier(ctx, opts)
	if err != nil {
		return err
	}

	resumeID, err := verifierAgent.NewResumeID(ctx)
	if err != nil {
		banner.DangerousFprintf(opts.Stderr, "J: warning: %v\n", err)
	}

	// The worker agent for the fix loop must match the tool the
	// task was originally worked with so the resume cursor lines
	// up. lookupResumeAgent resolves it; a missing entry surfaces
	// as a clean error.
	workerAgent, ok := lookupResumeAgent(opts.Agents, res.Task.InvokedTool)
	if !ok {
		return fmt.Errorf("J: unknown tool %q (recorded on task %s)", res.Task.InvokedTool, res.Task.ID)
	}

	lc := res.Task.BeginVerify(opts.Stderr, verifierAgent.Name(), model, resumeID)
	outcome, runErr := runVerifyLoop(ctx, opts, verifierAgent, workerAgent, model, resumeID, res)
	lc.Finish(outcome, runErr)
	if runErr != nil {
		return runErr
	}
	switch outcome {
	case store.VerifyOutcomeSuccess:
		banner.Fprintf(opts.Stdout, "J: verified task %s\n", res.Task.ID)
	case store.VerifyOutcomeNoRetries:
		banner.DangerousFprintf(opts.Stdout, "J: verifier exhausted retries on task %s; status verify-done\n", res.Task.ID)
	}
	return nil
}

// resolveTask implements the precedence: --from-task > most recent
// work-done auto-pick > UI picker over every task. Each branch
// returns a fully-populated resolved or a wrapped error.
//
// When the bbolt store contains exactly one row whose status is
// in the natural verify allowlist (work-done / verify-done / help),
// the no-flag path auto-picks it without prompting — this preserves
// the historic happy-path UX. Otherwise the picker shows every task
// and the downstream confirm prompt handles the wrong-status case.
//
// The bool return is the "proceed" signal from the unified
// picker contract: ok=false means the user cancelled the picker
// (Ctrl-C / Esc) and Run should exit cleanly without invoking the
// agent. ok=true means the resolved struct is populated and Run
// can continue.
// runVerifyLoop alternates verifier turns with worker-resume fix
// turns until the verifier writes VERDICT: PASS to findingsPath or
// MaxIterations is exhausted. Errors from either agent abort the
// loop and surface up to Run, which finishes the row as `help`.
//
// The body and verdict-on-FAIL semantics mirror the plan flowchart
// exactly: turn 1 always runs the verifier; subsequent iterations
// resume the worker with FixFindings populated, then re-run the
// verifier with Resume=true so the prior verification context is
// reused.
//
// The orchestrator blocks on every spawned child via run.WaitForExit
// before reading findings or queuing the next worker turn. The
// codingagents.Agent contract permits backends to return a non-zero
// PID for fire-and-forget headless children whose Wait was released;
// reading findings before the child has finished writing them would
// race the verdict line and produce a stale FAIL. The wait honours
// the contract documented on agent.go without binding child
// lifetime to ctx — see run.Spawn's commentary on why a true
// fire-and-forget child cannot be safely killed by ctx cancellation.
func runVerifyLoop(ctx context.Context, opts Options, verifierAgent, workerAgent codingagents.Agent, model, resumeID string, res resolved) (store.VerifyOutcome, error) {
	agentLogPath := filepath.Join(res.TaskDir, store.AgentLogFileName)
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		banner.DangerousFprintf(opts.Stderr, "J: warning: %v\n", mustReadErr)
	}
	for i := 0; i < opts.MaxIterations; i++ {
		req := codingagents.VerifyRequest{
			RequirementsPath:           res.RequirementsPath,
			PlanPath:                   res.PlanPath,
			VerifierPlanOutputPath:     res.VerifierPlanPath,
			VerifierFindingsOutputPath: res.FindingsPath,
			Model:                      model,
			Interactive:                opts.Interactive,
			Resume:                     i > 0,
			ResumeChatID:               resumeID,
			Worktree:                   res.Task.Worktree,
			AgentLogPath:               agentLogPath,
			MustRead:                   mustReadFiles,
		}
		pid, err := verifierAgent.Verify(ctx, req)
		if err != nil {
			return store.VerifyOutcomeNoRetries, err
		}
		if err := run.WaitForExit(ctx, pid); err != nil {
			return store.VerifyOutcomeNoRetries, err
		}
		verdict := ParseVerdict(res.FindingsPath)
		if verdict == "PASS" {
			return store.VerifyOutcomeSuccess, nil
		}
		// On FAIL we still need to keep iterating: break out
		// when the next loop turn would fall off the
		// MaxIterations cliff so we don't run a worker fix
		// turn whose verifier counterpart has nowhere to go.
		if i+1 >= opts.MaxIterations {
			break
		}
		workReq := codingagents.WorkRequest{
			PlanPath:     res.PlanPath,
			Model:        res.Task.InvokedModel,
			Interactive:  opts.Interactive,
			ResumeChatID: res.Task.WorkResumeCursor,
			Resume:       true,
			FixFindings:  true,
			Worktree:     res.Task.Worktree,
			AgentLogPath: agentLogPath,
		}
		workPID, err := workerAgent.Work(ctx, workReq)
		if err != nil {
			return store.VerifyOutcomeNoRetries, err
		}
		if err := run.WaitForExit(ctx, workPID); err != nil {
			return store.VerifyOutcomeNoRetries, err
		}
	}
	return store.VerifyOutcomeNoRetries, nil
}

// ReadVerdictForTask reads <cwd>/.j/tasks/<id>/verifier_findings.md
// via ParseVerdict and returns the terminal verdict ("PASS" / "FAIL").
// Any failure (missing tasks dir, missing file, malformed line)
// yields "FAIL" so callers treat ambiguity as a non-pass — matching
// the same rule ParseVerdict applies internally.
