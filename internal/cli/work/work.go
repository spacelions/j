// Package work implements the `j work` subcommand. It resolves a plan
// to execute (by --from-task <id>, --from-file, the most recent plan-done
// task in bbolt, or an interactive picker), prompts the user for a
// coding agent and model, verifies that backend is signed in, and
// hands the plan to the agent so it can edit files in place. The
// orchestrator does not write any output file.
package work

import (
	"context"
	"errors"
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
// supplied by the caller (the CLI wires the Cursor agent; tests inject
// scripted ones). Interactive selects the agent's TUI when true and
// the headless path when false.
type Options struct {
	// TaskID, when set, names an existing task whose plan.md should be
	// executed. The task row is updated in place (plan-done -> working
	// -> work-done|help). Beats FromFile when both are supplied.
	TaskID string
	// FromFile is a legacy escape hatch: the path of a plan markdown
	// file outside .j/tasks/. The file is imported into a fresh
	// .j/tasks/<new-id>/plan.md folder and a NEW task row is created.
	FromFile string
	// Yes, when true, skips the status-mismatch confirmation prompt
	// and proceeds even when the resolved task is not in the
	// plan-done / help allowlist. Mirrors the `--yes` / WORK_YES
	// flag wiring on the cobra command.
	Yes bool

	// Interactive is the resolved interactive flag. cobra cmd.go
	// computes it via resolver.Interactive (explicit > stored > true)
	// before constructing Options.
	Interactive bool

	// Tool and Model are one-off overrides for the worker bucket's
	// recorded tool/model. When either is set, Run resolves the
	// worker via resolver.Agent (filling the missing half from
	// the bucket if needed) and skips persistence: the bucket is
	// left untouched. When both are empty, Run falls back to the
	// existing read-then-prompt-then-persist precedence.
	Tool  string
	Model string

	// WaitForCompletion mirrors plan.Options.WaitForCompletion: blocks
	// on a returned non-zero PID and runs the work lifecycle's Finish
	// synchronously so the orchestrator chain can advance to the
	// verifier only after the worker exits.
	WaitForCompletion bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	// Store, when non-nil, receives best-effort writes recording the
	// tool/model/interactive flag last used. The work source is
	// intentionally NOT persisted: the user must supply or be
	// prompted for it every run. The orchestrator does not own the
	// lifecycle when the caller supplies a Store. When nil, the
	// helpers below open `<cwd>/.j/settings` only for the duration
	// of each individual read/write so the bbolt file lock is never
	// held across the long-running agent.Work invocation. Tests
	// that supply a Store directly skip the open/close cycle
	// entirely.
	Store *store.Store
}

// resolved is the outcome of resolvePlan: it tells Run which path to
// take for task logging and what to hand to the agent.
type resolved struct {
	// Existing is the bbolt task row to mutate when non-nil. When nil
	// the run is a legacy file import and a NEW task row is created.
	Existing *store.Task
	// PlanPath is the absolute path of the plan markdown to execute.
	// For Existing!=nil it is <cwd>/.j/tasks/<id>/plan.md; for legacy
	// imports it is <cwd>/.j/tasks/<new-id>/plan.md after the import.
	PlanPath string
	// Body is the plan markdown contents.
	Body string
	// Requirement is the requirement markdown body when available
	// (read from <cwd>/.j/tasks/<id>/requirements.md or, for legacy
	// imports, the `<stem>.md` sidecar of FromFile).
	Requirement string
	// NewTaskID, set only on the legacy file-import path, is the id
	// of the freshly created task folder.
	NewTaskID string
}

// Run executes `j work`. It resolves the plan source (Options.TaskID,
// Options.FromFile, latest plan-done bbolt row, then UI picker), then
// dispatches to the reuse or import path.
//
// User-abort signals from any huh prompt (Ctrl+C / Esc) propagate up
// as huh.ErrUserAborted; the deferred guard below converts them to a
// nil return so an explicit cancel exits the command cleanly without
// printing a bogus "cancelled by user" line. Genuine errors keep
// their original wrapping.
//
// The bbolt file lock on `<cwd>/.j/settings` is never held across the
// agent.Work call: each settings read/write below opens the DB,
// performs the operation, and closes before any agent work begins so
// concurrent `j settings` / `j tasks` invocations are not blocked on
// the OS file lock.
func Run(ctx context.Context, opts Options) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("J: no coding agents configured")
	}
	res, ok, err := resolvePlan(ctx, opts)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	agent, model, err := selectWorker(ctx, opts)
	if err != nil {
		return err
	}

	resumeID, err := agent.NewResumeID(ctx)
	if err != nil {
		banner.DangerousFprintf(opts.Stderr, "J: warning: %v\n", err)
	}

	var lc *store.WorkLifecycle
	if res.Existing != nil {
		proceed, confirmErr := confirmStatusOverride(ctx, opts, "work", *res.Existing, allowedForWork)
		if confirmErr != nil {
			return confirmErr
		}
		if !proceed {
			return nil
		}
		lc = res.Existing.BeginWorkReuse(opts.Stderr, agent.Name(), model, resumeID)
	} else {
		lc = store.NewWorkTask(opts.Stderr, agent.Name(), model, res.NewTaskID, res.PlanPath, res.Requirement, res.Body, resumeID)
	}

	agentLogPath := filepath.Join(filepath.Dir(res.PlanPath), store.AgentLogFileName)
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		banner.DangerousFprintf(opts.Stderr, "J: warning: %v\n", mustReadErr)
	}
	pid, workErr := agent.Work(ctx, codingagents.WorkRequest{
		PlanPath:     res.PlanPath,
		Model:        model,
		Interactive:  opts.Interactive,
		ResumeChatID: resumeID,
		Worktree:     lc.Task().Worktree,
		AgentLogPath: agentLogPath,
		MustRead:     mustReadFiles,
	})
	if workErr == nil && pid > 0 {
		if opts.WaitForCompletion {
			if err := run.WaitForExit(ctx, pid); err != nil {
				lc.Finish(err)
				return err
			}
		} else {
			lc.RecordBackground(pid, agentLogPath)
			banner.RunningInBackground(opts.Stdout, agent.Name(), pid, agentLogPath)
			return nil
		}
	}
	lc.Finish(workErr)
	if workErr != nil {
		return workErr
	}

	if res.Existing != nil {
		banner.Fprintf(opts.Stdout, "J: coding on task %s\n", res.Existing.ID)
	} else {
		banner.Fprintf(opts.Stdout, "J: coding on %s\n", res.PlanPath)
	}
	return nil
}

// resolvePlan implements the precedence: --from-task > --from-file (legacy
// import) > pick latest plan-done auto-pick > UI picker over every task.
// Each branch returns a fully-populated resolved or a wrapped error;
// callers do not need to re-stat or re-read files afterwards.
//
// When the bbolt store contains exactly one row whose status is in
// the natural work allowlist (plan-done / help), the no-flag path
// auto-picks it without prompting — this preserves the
// historic happy-path UX for the common case of a fresh `j plan`
// followed by `j work`. Otherwise the picker shows every task and
// the downstream confirm prompt handles the wrong-status case.
//
// The bool return is the "proceed" signal from the unified
// picker contract: ok=false means the user cancelled the picker
// (Ctrl-C / Esc) and the caller should exit cleanly without
// invoking the agent. ok=true means the resolved struct is
// populated and Run can continue.
