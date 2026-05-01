// Package verify implements the `j verify` subcommand. It resolves a
// work-done task to verify, prompts the user for a verifier agent /
// model, verifies that backend is signed in, and runs a bounded
// fix-loop alternating verifier turns with coder-resume turns until
// the verifier writes `VERDICT: PASS` to verifier_findings.md or the
// iteration cap is exhausted. The verifier writes verifier_plan.md
// and verifier_findings.md inside `<cwd>/.j/tasks/<id>/`; the coder
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
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
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

	// Interactive is a tri-state: a non-nil value is the explicit
	// user choice (cobra `--interactive` flag or VERIFY_INTERACTIVE
	// env var), and nil means "not set, fall back to the stored
	// `interactive` (when FromSettings is true) or the cobra
	// default true". Stored only wins when Interactive is nil and
	// FromSettings is true; explicit always wins.
	Interactive *bool

	// FromSettings, when true, makes Run reuse the tool/model
	// recorded in the verifier bucket of <cwd>/.j/settings instead
	// of prompting. Mirrors the `j plan` / `j work` semantics.
	FromSettings bool

	// MaxIterations bounds the verifier / coder-fix loop. Zero or
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
	RequirementsBody string
	PlanPath         string
	PlanBody         string
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

	res, err := resolveTask(ctx, opts)
	if err != nil {
		return err
	}
	if err := validateForVerify(res.Task); err != nil {
		return err
	}

	verifierAgent, model, err := selectVerifier(ctx, opts)
	if err != nil {
		return err
	}

	resumeID, err := verifierAgent.NewResumeID(ctx)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", err)
	}

	// The coder agent for the fix loop must match the tool the
	// task was originally worked with so the resume cursor lines
	// up. lookupResumeAgent resolves it; a missing entry surfaces
	// as a clean error.
	coderAgent, ok := lookupResumeAgent(opts.Agents, res.Task.InvokedTool)
	if !ok {
		return fmt.Errorf("J: unknown tool %q (recorded on task %s)", res.Task.InvokedTool, res.Task.ID)
	}

	lc := beginVerifyTask(opts, verifierAgent, model, res.Task, resumeID)
	outcome, runErr := runVerifyLoop(ctx, opts, verifierAgent, coderAgent, model, resumeID, res)
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
// work-done > UI picker over work-done / verify-done / help tasks.
// Each branch returns a fully-populated resolved or a wrapped
// error.
func resolveTask(ctx context.Context, opts Options) (resolved, error) {
	if opts.TaskID != "" {
		return resolveByTaskID(opts, opts.TaskID)
	}
	tasks, err := listVerifiableTasks(opts)
	if err != nil {
		return resolved{}, err
	}
	switch len(tasks) {
	case 0:
		return resolved{}, errors.New("J: no work-done tasks to verify; run `j work` first")
	case 1:
		return resolveByTaskID(opts, tasks[0].ID)
	}
	chosen, err := opts.UI.PickWorkDoneTask(ctx, tasks)
	if err != nil {
		return resolved{}, err
	}
	return resolveByTaskID(opts, chosen)
}

// resolveByTaskID loads an existing task row, then reads
// .j/tasks/<id>/{requirements,plan}.md (best-effort for
// requirements). The id is trusted (it came from a previous
// EnsureTaskDir call that staged the row).
func resolveByTaskID(opts Options, id string) (resolved, error) {
	s, ok := openTaskLog(opts.Stderr)
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
	planBody, err := os.ReadFile(planPath)
	if err != nil {
		return resolved{}, fmt.Errorf("verify: read plan: %w", err)
	}
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	var requirementsBody string
	if data, readErr := os.ReadFile(requirementsPath); readErr == nil {
		requirementsBody = string(data)
	}
	return resolved{
		Task:             task,
		TaskDir:          taskDir,
		RequirementsPath: requirementsPath,
		RequirementsBody: requirementsBody,
		PlanPath:         planPath,
		PlanBody:         string(planBody),
		VerifierPlanPath: filepath.Join(taskDir, store.VerifierPlanFileName),
		FindingsPath:     filepath.Join(taskDir, store.VerifierFindingsFileName),
	}, nil
}

// listVerifiableTasks returns every task whose status is in the
// validateForVerify allowlist (work-done / verify-done / help)
// sorted via store.SortTasks so the picker shows the active-then-
// most-recent order users see in `j tasks`.
func listVerifiableTasks(opts Options) ([]store.Task, error) {
	s, ok := openTaskLog(opts.Stderr)
	if !ok {
		return nil, errors.New("verify: tasks db unavailable")
	}
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	store.SortTasks(all)
	out := all[:0]
	for _, t := range all {
		if validateForVerify(t) == nil {
			out = append(out, t)
		}
	}
	return out, nil
}

// validateForVerify rejects starting `j verify` against a task whose
// status would clobber unrelated state. Allowed entry statuses are
// work-done (the happy path), verify-done (re-verify after an
// exhausted loop), and help (retry after a failed run). Anything
// else is a deterministic error.
func validateForVerify(t store.Task) error {
	switch t.Status {
	case store.StatusWorkDone, store.StatusVerifyDone, store.StatusHelp:
		return nil
	case store.StatusPlanning:
		return fmt.Errorf("verify: task %s is still planning", t.ID)
	case store.StatusPlanDone:
		return fmt.Errorf("verify: task %s has not been worked yet (status plan-done)", t.ID)
	case store.StatusWorking:
		return fmt.Errorf("verify: task %s is still working", t.ID)
	case store.StatusVerifying:
		return fmt.Errorf("verify: task %s is already verifying", t.ID)
	case store.StatusCompleted:
		return fmt.Errorf("verify: task %s is already completed", t.ID)
	}
	return fmt.Errorf("verify: task %s has unsupported status %q", t.ID, t.Status)
}

// runVerifyLoop alternates verifier turns with coder-resume fix
// turns until the verifier writes VERDICT: PASS to findingsPath or
// MaxIterations is exhausted. Errors from either agent abort the
// loop and surface up to Run, which finishes the row as `help`.
//
// The body and verdict-on-FAIL semantics mirror the plan flowchart
// exactly: turn 1 always runs the verifier; subsequent iterations
// resume the coder with FixFindings populated, then re-run the
// verifier with Resume=true so the prior verification context is
// reused.
func runVerifyLoop(ctx context.Context, opts Options, verifierAgent, coderAgent codingagents.Agent, model, resumeID string, res resolved) (verifyOutcome, error) {
	agentLogPath := filepath.Join(res.TaskDir, agentLogFileName)
	for i := 0; i < opts.MaxIterations; i++ {
		req := codingagents.VerifyRequest{
			RequirementsPath:           res.RequirementsPath,
			RequirementsBody:           res.RequirementsBody,
			PlanPath:                   res.PlanPath,
			PlanBody:                   res.PlanBody,
			VerifierPlanOutputPath:     res.VerifierPlanPath,
			VerifierFindingsOutputPath: res.FindingsPath,
			PreviousFindings:           readBestEffort(res.FindingsPath),
			Model:                      model,
			Interactive:                *opts.Interactive,
			Resume:                     i > 0,
			ResumeChatID:               resumeID,
			AgentLogPath:               agentLogPath,
		}
		if _, err := verifierAgent.Verify(ctx, req); err != nil {
			return outcomeNoRetries, err
		}
		verdict := parseVerdict(res.FindingsPath)
		if verdict == "PASS" {
			return outcomeSuccess, nil
		}
		// On FAIL we still need to keep iterating: break out
		// when the next loop turn would fall off the
		// MaxIterations cliff so we don't run a coder fix
		// turn whose verifier counterpart has nowhere to go.
		if i+1 >= opts.MaxIterations {
			break
		}
		findingsBody := readBestEffort(res.FindingsPath)
		workReq := codingagents.WorkRequest{
			PlanPath:     res.PlanPath,
			Body:         res.PlanBody,
			Model:        res.Task.InvokedModel,
			Interactive:  *opts.Interactive,
			ResumeChatID: res.Task.WorkResumeCursor,
			Resume:       true,
			FixFindings:  findingsBody,
			AgentLogPath: agentLogPath,
		}
		if _, err := coderAgent.Work(ctx, workReq); err != nil {
			return outcomeNoRetries, err
		}
	}
	return outcomeNoRetries, nil
}

// parseVerdict reads path and inspects its last non-empty line for
// the literal `VERDICT: PASS` / `VERDICT: FAIL` marker. Any other
// shape (missing file, empty file, malformed line, trailing prose)
// yields "FAIL" so the orchestrator treats ambiguity as a failure.
// The match is case-insensitive on PASS / FAIL and tolerates
// surrounding whitespace.
func parseVerdict(path string) string {
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
// tool/model. Mirrors selectCoder in `j work`: when FromSettings is
// true it tries the read-only agentpick.FromStore path first and
// only falls back to the interactive Pick flow on
// ErrNoStoredSelection.
func selectVerifier(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.FromSettings {
		agent, model, err := verifierFromSettings(ctx, opts)
		if err == nil {
			return agent, model, nil
		}
		if !errors.Is(err, agentpick.ErrNoStoredSelection) {
			return nil, "", err
		}
		fmt.Fprintln(opts.Stderr, "Choose your favourite:")
	}
	agent, model, err := agentpick.Pick(ctx, opts.UI, opts.Agents)
	if err != nil {
		return nil, "", err
	}
	persistVerifierSelection(opts, agent.Name(), model)
	return agent, model, nil
}

// verifierFromSettings reads the verifier bucket and returns the
// chosen tool/model. Mirrors coderFromSettings in `j work`.
func verifierFromSettings(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.Store != nil {
		return agentpick.FromStore(ctx, opts.Store, store.BucketVerifier, opts.Agents)
	}
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return nil, "", agentpick.ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return agentpick.FromStore(ctx, s, store.BucketVerifier, opts.Agents)
}

// persistVerifierSelection writes the just-confirmed tool/model and
// the interactive flag to the verifier bucket. Mirrors
// persistCoderSelection in `j work`.
func persistVerifierSelection(opts Options, tool, model string) {
	interactive := true
	if opts.Interactive != nil {
		interactive = *opts.Interactive
	}
	if opts.Store != nil {
		store.PersistAgentSelection(opts.Store, opts.Stderr, store.BucketVerifier, tool, model, interactive)
		return
	}
	s, ok := openSettingsStore(opts.Stderr)
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
	if opts.FromSettings {
		if v, ok := storedVerifierInteractive(opts); ok {
			return v
		}
	}
	return true
}

// storedVerifierInteractive looks up the verifier bucket's
// `interactive` entry. Mirrors storedCoderInteractive in `j work`.
func storedVerifierInteractive(opts Options) (bool, bool) {
	if opts.Store != nil {
		return agentpick.StoredInteractive(opts.Store, store.BucketVerifier)
	}
	s, ok := openSettingsStore(opts.Stderr)
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

// readBestEffort reads path silently. Errors yield an empty string
// because the verify flow tolerates a missing file (e.g. the
// verifier crashed before writing findings).
func readBestEffort(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
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
		o.UI = newHuhUI(o.Stdin, o.Stderr)
	}
	if o.MaxIterations <= 0 {
		o.MaxIterations = defaultMaxIterations
	}
	return o
}

// openSettingsStore opens `<cwd>/.j/settings` for the verifier flow.
// Mirrors openSettingsStore in `j work`.
func openSettingsStore(stderr io.Writer) (*store.Store, bool) {
	path, err := store.DefaultPath()
	if err != nil {
		fmt.Fprintf(stderr, "warning: settings path: %v\n", err)
		return nil, false
	}
	s, err := store.Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: settings db: %v\n", err)
		return nil, false
	}
	return s, true
}

// openTaskLog opens `<cwd>/.j/tasks/list.db` for the verify flow.
// Mirrors openTaskLog in `j work`.
func openTaskLog(stderr io.Writer) (*store.Store, bool) {
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks path: %v\n", err)
		return nil, false
	}
	s, err := store.Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks db: %v\n", err)
		return nil, false
	}
	return s, true
}
