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
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/agentpick"
	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/tasklog"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/mustread"
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

	// Interactive is a tri-state: a non-nil value is the explicit
	// user choice (cobra `--interactive` flag or VERIFY_INTERACTIVE
	// env var), and nil means "not set, fall back to the stored
	// `interactive` value or the cobra default true". Stored wins
	// when Interactive is nil and the bucket has a parseable value;
	// explicit always wins.
	Interactive *bool

	// Tool and Model are one-off overrides for the verifier
	// bucket's recorded tool/model. When either is set, Run resolves
	// the verifier via agentpick.Resolve (filling the missing half
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

// verdictRegexp matches the literal terminal `VERDICT: PASS|FAIL`
// line case-insensitively on PASS / FAIL, tolerating surrounding
// whitespace. It is anchored so a stray `VERDICT: maybe` does not
// silently coerce to PASS.
var verdictRegexp = regexp.MustCompile(`(?i)^\s*VERDICT:\s*(PASS|FAIL)\s*$`)

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
	defer func() {
		if errors.Is(err, huh.ErrUserAborted) {
			err = nil
		}
	}()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("J: no coding agents configured")
	}
	opts.Interactive = boolPtr(resolveInteractive(opts))

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
		fmt.Fprintf(opts.Stderr, "warning: %v\n", err)
	}

	// The worker agent for the fix loop must match the tool the
	// task was originally worked with so the resume cursor lines
	// up. lookupResumeAgent resolves it; a missing entry surfaces
	// as a clean error.
	workerAgent, ok := lookupResumeAgent(opts.Agents, res.Task.InvokedTool)
	if !ok {
		return fmt.Errorf("J: unknown tool %q (recorded on task %s)", res.Task.InvokedTool, res.Task.ID)
	}

	lc := beginVerifyTask(opts, verifierAgent, model, res.Task, resumeID)
	outcome, runErr := runVerifyLoop(ctx, opts, verifierAgent, workerAgent, model, resumeID, res)
	lc.finishVerify(outcome, runErr)
	if runErr != nil {
		return runErr
	}
	switch outcome {
	case outcomeSuccess:
		fmt.Fprintf(opts.Stdout, "J: verified task %s\n", res.Task.ID)
	case outcomeNoRetries:
		fmt.Fprintf(opts.Stdout, "J: verifier exhausted retries on task %s; status verify-done\n", res.Task.ID)
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
// taskpick contract: ok=false means the user cancelled the picker
// (Ctrl-C / Esc) and Run should exit cleanly without invoking the
// agent. ok=true means the resolved struct is populated and Run
// can continue.
func resolveTask(ctx context.Context, opts Options) (resolved, bool, error) {
	if opts.TaskID != "" {
		r, err := resolveByTaskID(opts, opts.TaskID)
		return r, err == nil, err
	}
	tasks, err := listResolvableVerifyTasks(opts)
	if err != nil {
		return resolved{}, false, err
	}
	if len(tasks) == 0 {
		return resolved{}, false, errors.New("J: no tasks to verify; run `j plan` and `j work` first")
	}
	if id, ok := autoPickAllowed(tasks, allowedForVerify); ok {
		r, err := resolveByTaskID(opts, id)
		return r, err == nil, err
	}
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a task to verify", tasks)
	if err != nil {
		return resolved{}, false, err
	}
	if !ok {
		return resolved{}, false, nil
	}
	r, err := resolveByTaskID(opts, chosen)
	return r, err == nil, err
}

// autoPickAllowed returns the single task id and ok=true when
// exactly one of the supplied tasks is in the allowlist (i.e. the
// happy-path auto-pick). Any other count surfaces ok=false so the
// caller renders the picker over the full slice.
func autoPickAllowed(tasks []store.Task, allowed func(store.Task) bool) (string, bool) {
	var picked string
	count := 0
	for _, t := range tasks {
		if allowed(t) {
			picked = t.ID
			count++
		}
	}
	if count == 1 {
		return picked, true
	}
	return "", false
}

// resolveByTaskID loads an existing task row, then reads
// .j/tasks/<id>/{requirements,plan}.md (best-effort for
// requirements). The id is trusted (it came from a previous
// EnsureTaskDir call that staged the row).
func resolveByTaskID(opts Options, id string) (resolved, error) {
	s, ok := tasklog.OpenTaskLog(opts.Stderr)
	if !ok {
		return resolved{}, errors.New("verify: tasks db unavailable")
	}
	defer func() { _ = s.Close() }()
	task, err := s.GetTask(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return resolved{}, fmt.Errorf("verify: task %q not found", id)
		}
		return resolved{}, err
	}
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return resolved{}, err
	}
	taskDir := filepath.Join(tasksDir, id)
	planPath := filepath.Join(taskDir, store.PlanFileName)
	if _, err := os.Stat(planPath); err != nil {
		return resolved{}, fmt.Errorf("verify: read plan: %w", err)
	}
	return resolved{
		Task:             task,
		TaskDir:          taskDir,
		RequirementsPath: filepath.Join(taskDir, store.RequirementsFileName),
		PlanPath:         planPath,
		VerifierPlanPath: filepath.Join(taskDir, store.VerifierPlanFileName),
		FindingsPath:     filepath.Join(taskDir, store.VerifierFindingsFileName),
	}, nil
}

// listResolvableVerifyTasks returns every task in bbolt sorted via
// store.SortTasks. The picker surfaces every row regardless of
// status; the downstream confirm prompt handles the wrong-status
// case (per the re-verify contract). autoPickAllowed auto-picks
// when exactly one row is in the verify allowlist.
func listResolvableVerifyTasks(opts Options) ([]store.Task, error) {
	s, ok := tasklog.OpenTaskLog(opts.Stderr)
	if !ok {
		return nil, errors.New("verify: tasks db unavailable")
	}
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	store.SortTasks(all)
	return all, nil
}

// allowedForVerify is the natural status allowlist for `j verify`:
// work-done (the happy path after `j work`), verify-done (re-verify
// after an exhausted loop), and help (retry after a failed run).
// Anything else is allowed by `j verify` only after the user
// confirms the prompt (or via --yes / VERIFY_YES); this preserves
// the prior UX for the common case while letting users re-run
// verify against in-flight or post-verify tasks.
func allowedForVerify(t store.Task) bool {
	switch t.Status {
	case store.StatusWorkDone, store.StatusVerifyDone, store.StatusHelp:
		return true
	}
	return false
}

// confirmStatusOverride decides whether to run agent.Verify against
// a task whose status falls outside the allowlist. Allowlist match
// → proceed silently. Otherwise --yes / VERIFY_YES → proceed
// silently; else delegate to the UI confirm prompt and return its
// bool. A user decline (false from the prompt) returns
// proceed=false with err=nil so the caller can exit cleanly.
func confirmStatusOverride(ctx context.Context, opts Options, cmd string, t store.Task, allowed func(store.Task) bool) (bool, error) {
	if allowed(t) {
		return true, nil
	}
	if opts.Yes {
		return true, nil
	}
	return opts.UI.ConfirmStatusOverride(ctx, cmd, t.ID, string(t.Status))
}

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
func runVerifyLoop(ctx context.Context, opts Options, verifierAgent, workerAgent codingagents.Agent, model, resumeID string, res resolved) (verifyOutcome, error) {
	agentLogPath := filepath.Join(res.TaskDir, tasklog.AgentLogFileName)
	mustReadFiles, mustReadErr := mustread.LoadFromDefault()
	if mustReadErr != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", mustReadErr)
	}
	for i := 0; i < opts.MaxIterations; i++ {
		req := codingagents.VerifyRequest{
			RequirementsPath:           res.RequirementsPath,
			PlanPath:                   res.PlanPath,
			VerifierPlanOutputPath:     res.VerifierPlanPath,
			VerifierFindingsOutputPath: res.FindingsPath,
			Model:                      model,
			Interactive:                *opts.Interactive,
			Resume:                     i > 0,
			ResumeChatID:               resumeID,
			Worktree:                   res.Task.Worktree,
			AgentLogPath:               agentLogPath,
			MustRead:                   mustReadFiles,
		}
		pid, err := verifierAgent.Verify(ctx, req)
		if err != nil {
			return outcomeNoRetries, err
		}
		if err := run.WaitForExit(ctx, pid); err != nil {
			return outcomeNoRetries, err
		}
		verdict := ParseVerdict(res.FindingsPath)
		if verdict == "PASS" {
			return outcomeSuccess, nil
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
			Interactive:  *opts.Interactive,
			ResumeChatID: res.Task.WorkResumeCursor,
			Resume:       true,
			FixFindings:  true,
			Worktree:     res.Task.Worktree,
			AgentLogPath: agentLogPath,
		}
		workPID, err := workerAgent.Work(ctx, workReq)
		if err != nil {
			return outcomeNoRetries, err
		}
		if err := run.WaitForExit(ctx, workPID); err != nil {
			return outcomeNoRetries, err
		}
	}
	return outcomeNoRetries, nil
}

// ReadVerdictForTask reads <cwd>/.j/tasks/<id>/verifier_findings.md
// via ParseVerdict and returns the terminal verdict ("PASS" / "FAIL").
// Any failure (missing tasks dir, missing file, malformed line)
// yields "FAIL" so callers treat ambiguity as a non-pass — matching
// the same rule ParseVerdict applies internally.
func ReadVerdictForTask(taskID string) string {
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return "FAIL"
	}
	return ParseVerdict(filepath.Join(tasksDir, taskID, store.VerifierFindingsFileName))
}

// ParseVerdict reads path and inspects its last non-empty line for
// the literal `VERDICT: PASS` / `VERDICT: FAIL` marker. Any other
// shape (missing file, empty file, malformed line, trailing prose)
// yields "FAIL" so the orchestrator treats ambiguity as a failure.
// The match is case-insensitive on PASS / FAIL and tolerates
// surrounding whitespace.
func ParseVerdict(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "FAIL"
	}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := verdictRegexp.FindStringSubmatch(line)
		if m == nil {
			return "FAIL"
		}
		return strings.ToUpper(m[1])
	}
	return "FAIL"
}

// selectVerifier is the single chokepoint for choosing the verifier
// tool/model. Mirrors selectWorker in `j work`. Precedence:
//  1. explicit --tool / --model → agentpick.Resolve fills the missing
//     half from the verifier bucket; bucket is NOT written.
//  2. populated verifier bucket → agentpick.FromStore reuses it.
//  3. otherwise → agentpick.Pick prompts the user and the result is
//     persisted to the verifier bucket.
func selectVerifier(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.Tool != "" || opts.Model != "" {
		agent, model, err := verifierResolveExplicit(ctx, opts)
		if err == nil {
			return agent, model, nil
		}
		if !errors.Is(err, agentpick.ErrNoStoredSelection) {
			return nil, "", err
		}
	}
	agent, model, err := verifierFromStore(ctx, opts)
	if err == nil {
		return agent, model, nil
	}
	if !errors.Is(err, agentpick.ErrNoStoredSelection) {
		return nil, "", err
	}
	fmt.Fprintln(opts.Stderr, "Choose your favourite:")
	agent, model, err = agentpick.Pick(ctx, opts.UI, opts.Agents)
	if err != nil {
		return nil, "", err
	}
	persistVerifierSelection(opts, agent.Name(), model)
	return agent, model, nil
}

// verifierResolveExplicit reads the verifier bucket only to fill the
// missing half of the user-supplied --tool / --model pair. Mirrors
// workerResolveExplicit in `j work`.
func verifierResolveExplicit(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.Store != nil {
		return agentpick.Resolve(ctx, opts.Store, store.BucketVerifier, opts.Agents, opts.Tool, opts.Model)
	}
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return agentpick.Resolve(ctx, nil, store.BucketVerifier, opts.Agents, opts.Tool, opts.Model)
	}
	defer func() { _ = s.Close() }()
	return agentpick.Resolve(ctx, s, store.BucketVerifier, opts.Agents, opts.Tool, opts.Model)
}

// verifierFromStore reads the verifier bucket and returns the
// chosen tool/model. Mirrors workerFromStore in `j work`.
func verifierFromStore(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.Store != nil {
		return agentpick.FromStore(ctx, opts.Store, store.BucketVerifier, opts.Agents)
	}
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return nil, "", agentpick.ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return agentpick.FromStore(ctx, s, store.BucketVerifier, opts.Agents)
}

// persistVerifierSelection writes the tool/model and interactive flag
// to the verifier bucket. Mirrors persistWorkerSelection in `j work`.
func persistVerifierSelection(opts Options, tool, model string) {
	interactive := true
	if opts.Interactive != nil {
		interactive = *opts.Interactive
	}
	if opts.Store != nil {
		store.PersistAgentSelection(opts.Store, opts.Stderr, store.BucketVerifier, tool, model, interactive)
		return
	}
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()
	store.PersistAgentSelection(s, opts.Stderr, store.BucketVerifier, tool, model, interactive)
}

// resolveInteractive applies the documented precedence (explicit >
// stored > cobra default true) and returns a concrete bool.
func resolveInteractive(opts Options) bool {
	if opts.Interactive != nil {
		return *opts.Interactive
	}
	if v, ok := storedVerifierInteractive(opts); ok {
		return v
	}
	return true
}

// storedVerifierInteractive looks up the verifier bucket's
// `interactive` entry. Mirrors storedWorkerInteractive in `j work`.
func storedVerifierInteractive(opts Options) (bool, bool) {
	if opts.Store != nil {
		return agentpick.StoredInteractive(opts.Store, store.BucketVerifier)
	}
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return false, false
	}
	defer func() { _ = s.Close() }()
	return agentpick.StoredInteractive(s, store.BucketVerifier)
}

// boolPtr is the package-private companion that lets Run / tests
// build a non-nil *bool from a literal without spelling out a temp
// variable at every call site.
func boolPtr(b bool) *bool { return &b }

// lookupResumeAgent returns the first agent in agents whose Name
// matches tool. The miss path becomes the user-facing
// "unknown tool %q" error in Run / RunResume.
func lookupResumeAgent(agents []codingagents.Agent, tool string) (codingagents.Agent, bool) {
	for _, a := range agents {
		if a.Name() == tool {
			return a, true
		}
	}
	return nil, false
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

