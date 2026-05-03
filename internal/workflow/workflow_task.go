package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/spacelions/j/internal/cli/tasklog"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/workflow/agents/planner"
	"github.com/spacelions/j/internal/workflow/agents/verifier"
	"github.com/spacelions/j/internal/workflow/agents/worker"
)

// defaultTaskMaxIterations matches `j verify`'s internal default and
// is the fallback when the project setting is missing or unparseable.
const defaultTaskMaxIterations = 3

// orchestratorAppName / orchestratorUserID name the synthetic
// runner.Run session the SequentialAgent runs inside. The IDs are
// internal to the orchestrator; nothing user-visible depends on
// them, but pinning them here keeps the runner.New / Create call
// pair self-consistent.
const (
	orchestratorAppName = "j-tasks-orchestrate"
	orchestratorUserID  = "j-tasks"
)

// TaskConfig is the relaxed runtime config consumed by RunForTask.
// Only MaxIterations is meaningful; the Gemini knobs that LoadConfig
// demands (`project.api_key`, `project.model`) are intentionally
// absent because the shell-out path never instantiates a Gemini
// model — the actual LLM calls happen inside the cursor / claude
// binaries that the per-phase machinery spawns.
type TaskConfig struct {
	// MaxIterations bounds the verifier's internal worker→verifier
	// fix loop. Defaults to defaultTaskMaxIterations (3) when the
	// project setting is unset / unparseable / zero.
	MaxIterations int
}

// LoadConfigForTask reads only `project.max_iterations` from the
// per-project bbolt settings store. Missing file or missing key
// surface as the documented default (3) so a fresh project can run
// `j tasks start` end to end without setting any project knobs. A
// genuine bbolt open / read error still surfaces verbatim; only the
// "no settings yet" / "no value yet" cases are silently defaulted.
func LoadConfigForTask() (TaskConfig, error) {
	cfg := TaskConfig{MaxIterations: defaultTaskMaxIterations}
	path, err := store.DefaultPath()
	if err != nil {
		return TaskConfig{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return TaskConfig{}, fmt.Errorf("workflow: stat %q: %w", path, err)
	}
	s, err := store.Open(path)
	if err != nil {
		return TaskConfig{}, fmt.Errorf("workflow: open settings: %w", err)
	}
	defer func() { _ = s.Close() }()
	raw, err := readSetting(s, "max_iterations")
	if err != nil {
		return TaskConfig{}, err
	}
	if raw == "" {
		return cfg, nil
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || n == 0 {
		return cfg, nil
	}
	cfg.MaxIterations = int(n)
	return cfg, nil
}

// RunForTask drives the planner → worker → verifier flow for an
// already-seeded task end to end. The shape is a top-level
// SequentialAgent over three shell-out custom agents constructed
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
// in `verifying` (i.e. FAIL on the last iteration with no error
// surfaced) — flipped to `verify-done` so `j tasks` reflects the
// terminal outcome without waiting for the reaper. The PASS path
// is already finalised inside `verify.Run` to `completed`; the
// `help` path is finalised by the failing phase's lifecycle. So
// the only post-iter mop-up is the verify-FAIL / no-error case.
func RunForTask(ctx context.Context, cfg TaskConfig, taskID string, agents []codingagents.Agent, stderr io.Writer) error {
	if taskID == "" {
		return errors.New("workflow: task id required")
	}
	if len(agents) == 0 {
		return errors.New("workflow: no coding agents configured")
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaultTaskMaxIterations
	}

	plannerAgent, err := planner.New(planner.Config{
		TaskID: taskID,
		Agents: agents,
		Stderr: stderr,
	})
	if err != nil {
		return fmt.Errorf("workflow: planner: %w", err)
	}
	workerAgent, err := worker.New(worker.Config{
		TaskID: taskID,
		Agents: agents,
		Stderr: stderr,
	})
	if err != nil {
		return fmt.Errorf("workflow: worker: %w", err)
	}
	verifierAgent, err := verifier.New(verifier.Config{
		TaskID:        taskID,
		Agents:        agents,
		Stderr:        stderr,
		MaxIterations: cfg.MaxIterations,
	})
	if err != nil {
		return fmt.Errorf("workflow: verifier: %w", err)
	}

	root, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "planner_worker_verifier_task",
			Description: "Drives planner → worker → verifier for a single seeded task.",
			SubAgents:   []agent.Agent{plannerAgent, workerAgent, verifierAgent},
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

// driveSequential constructs the smallest viable runner.New session
// the SequentialAgent can run inside and drains the resulting event
// iterator. The first error short-circuits with a wrapping that
// names the offending phase (its agent.Author).
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

// finaliseVerifyFailIfStuck flips a row that is still pinned at
// `verifying` to `verify-done` after the orchestrator's
// SequentialAgent drains. This covers the verifier-shell-FAIL /
// no-error branch: verify.Run returned nil because the loop merely
// exhausted iterations on FAIL, and `outcomeNoRetries` was already
// finalised to `verify-done` by `verify.Run`'s finishVerify — but
// when the final findings file is missing or unreadable we still
// want the orchestrator to leave the row in a terminal state
// rather than `verifying`. Best-effort: any read / write error
// surfaces as a single warning on stderr and the helper returns.
func finaliseVerifyFailIfStuck(stderr io.Writer, taskID string) {
	s, ok := tasklog.OpenTaskLog(stderr)
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()
	t, err := s.GetTask(taskID)
	if err != nil {
		return
	}
	if t.Status != store.StatusVerifying {
		return
	}
	t.Status = store.StatusVerifyDone
	if err := s.PutTask(t); err != nil {
		fmt.Fprintf(stderr, "warning: tasks put: %v\n", err)
	}
}
