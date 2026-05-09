// Package deepseek implements the codingagents.Agent backed by the
// local deepseek-tui CLI (https://github.com/Hmbown/DeepSeek-TUI). It
// is the third concrete backend alongside internal/coding-agents/cursor
// and internal/coding-agents/claude and is wired into the same picker
// shown by `j plan / j work / j verify`.
//
// Unlike cursor (mints an id pre-run via `create-chat`) and claude
// (binds a locally-minted UUID via `--session-id`), deepseek-tui has
// no pre-run session-id binding flag. NewResumeID always returns
// ("", nil); the post-run id is captured by CaptureResumeID (see
// capture.go) which scans `~/.deepseek/sessions/*.json` for the
// session whose metadata.workspace matches and metadata.created_at
// is at or after the phase's begin-at timestamp.
package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/util/run"
)

// Binary is the deepseek-tui executable name.
const Binary = "deepseek-tui"

const (
	argWorkspace = "-w"
	argResume    = "-r"
	argExec      = "exec"
	argModel     = "--model"
	argAuto      = "--auto"
)

// defaultModels is the picker list shown for `j plan / j work / j verify`.
// deepseek-tui has no `--list-models` equivalent so we surface a stable
// canonical default. Users who want a different specific id can pin it
// via `j settings set <bucket>.model=<id>`.
//
//nolint:goconst // "deepseek-v4-pro" count inflated by test fixtures
var defaultModels = []string{"deepseek-v4-pro"}

// Agent is a deepseek-tui-backed worker. It is stateless: every method
// shells out to the real deepseek-tui binary on PATH via the run
// package's package-level helpers. Tests drive it with a stub binary on
// PATH rather than an injected runner (see AGENTS.md "no test seams").
type Agent struct{}

var (
	_ codingagents.Agent            = (*Agent)(nil)
	_ codingagents.ResumeIDCapturer = (*Agent)(nil)
)

// New returns a deepseek agent that shells out to the deepseek-tui CLI.
func New() *Agent { return &Agent{} }

// Name implements codingagents.Agent.
func (*Agent) Name() string { return "deepseek" }

// NewResumeID always returns ("", nil): deepseek-tui mints the session
// id only after the first turn writes its metadata to disk, so there
// is nothing to bind pre-run. The id is captured post-run by
// CaptureResumeID (capture.go) and threaded into later runs via
// PlanRequest/WorkRequest/VerifyRequest.ResumeChatID.
func (*Agent) NewResumeID(_ context.Context) (string, error) {
	return "", nil
}

// ListModels returns the static canonical alias set the picker shows.
// It does not shell out: deepseek-tui has no list command and the set
// is small and stable. The returned slice is a fresh copy so callers
// cannot mutate the package-level state.
func (*Agent) ListModels(_ context.Context) ([]string, error) {
	out := make([]string, len(defaultModels))
	copy(out, defaultModels)
	return out, nil
}

// doctorReport is the subset of `deepseek-tui doctor --json` we
// inspect. The CLI emits more fields; this struct only decodes what
// CheckLogin needs so a future schema addition does not break us.
type doctorReport struct {
	APIKey struct {
		Source string `json:"source"`
	} `json:"api_key"`
	ConfigPresent bool `json:"config_present"`
}

// errNotLoggedIn is the shared remediation message CheckLogin returns
// for any logged-out state (missing config, empty api-key source, or
// unparseable doctor output).
var errNotLoggedIn = errors.New(
	"deepseek-tui reports not logged in; run 'deepseek-tui login'",
)

// CheckLogin verifies the user is signed in to deepseek-tui. The CLI's
// `doctor --json` subcommand prints a JSON status block; a missing
// config or empty api-key source both indicate logged-out, and a
// non-zero exit propagates wrapped with the remediation hint.
func (*Agent) CheckLogin(ctx context.Context) error {
	out, err := run.Output(ctx, Binary, "doctor", "--json")
	if err != nil {
		return fmt.Errorf(
			"deepseek-tui doctor failed: %w "+
				"(run 'deepseek-tui login')",
			err,
		)
	}
	report, ok := parseDoctor(out)
	if !ok {
		return errNotLoggedIn
	}
	if !report.ConfigPresent || report.APIKey.Source == "" {
		return errNotLoggedIn
	}
	return nil
}

// parseDoctor decodes the `doctor --json` output. A decode failure or
// empty payload yields ok=false so CheckLogin treats the situation as
// logged-out instead of crashing on a future schema change.
func parseDoctor(out string) (doctorReport, bool) {
	out = strings.TrimSpace(out)
	if out == "" {
		return doctorReport{}, false
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		return doctorReport{}, false
	}
	return report, true
}

// Plan runs deepseek-tui against req. Two flavours are supported:
//
//   - Interactive: launch the TUI with `-w <workspace>` and the
//     optional `-r <id>` for a resume run. The model is left to the
//     TUI's configured default since `--model` is exposed only on
//     `exec`.
//   - Headless: `-w <workspace> [-r <id>] exec --model <m> --auto -- <prompt>`.
//     `--auto` auto-approves writes; `--yolo` (top-level shell-execution
//     toggle) is intentionally omitted to keep the permission boundary
//     tighter.
func (*Agent) Plan(
	ctx context.Context, req codingagents.PlanRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.FromFilePath)
	prompt := prompts.PlanPrompt(req)
	return runPhase(
		ctx, req.Interactive, workspace, req.ResumeChatID,
		req.Model, prompt, req.AgentLogPath,
	)
}

// Work runs deepseek-tui against a previously generated plan markdown.
func (*Agent) Work(
	ctx context.Context, req codingagents.WorkRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.PlanPath)
	prompt := prompts.WorkPrompt(req)
	return runPhase(
		ctx, req.Interactive, workspace, req.ResumeChatID,
		req.Model, prompt, req.AgentLogPath,
	)
}

// Verify runs deepseek-tui against the requirements + plan pair. Like
// cursor / claude, the verifier uses the project root workspace so it
// can `git worktree list` the target worktree.
func (*Agent) Verify(
	ctx context.Context, req codingagents.VerifyRequest,
) (int, error) {
	workspace := codingagents.ProjectRootWorkspace()
	prompt := prompts.VerifyPrompt(req)
	return runPhase(
		ctx, req.Interactive, workspace, req.ResumeChatID,
		req.Model, prompt, req.AgentLogPath,
	)
}

// runPhase is the shared dispatcher for Plan / Work / Verify. The
// interactive vs headless split is identical across phases — only the
// workspace and prompt differ — so threading them through this helper
// keeps every phase under the 80-line method cap.
func runPhase(
	ctx context.Context, interactive bool,
	workspace, resumeID, model, prompt, agentLogPath string,
) (int, error) {
	if interactive {
		args := topArgs(workspace, resumeID)
		if err := run.Run(ctx, Binary, args...); err != nil {
			return 0, fmt.Errorf("deepseek-tui: %w", err)
		}
		return 0, nil
	}
	args := append(
		topArgs(workspace, resumeID),
		execArgs(model, prompt)...,
	)
	// deepseek-tui's TUI prints its full reasoning + tool-call trace
	// to stdout, so plain run.Spawn captures the agent.log content we
	// want without any extra flag. claude / cursor go through
	// `--output-format stream-json` because their headless mode
	// otherwise collapses the run down to the final assistant text.
	pid, err := run.Spawn(ctx, agentLogPath, Binary, args...)
	if err != nil {
		return 0, fmt.Errorf("deepseek-tui: %w", err)
	}
	return pid, nil
}

// topArgs returns the leading argv segment shared by interactive and
// headless runs: `-w <workspace>` plus the optional `-r <id>`. Empty
// resumeID yields a 2-element slice (no resume).
func topArgs(workspace, resumeID string) []string {
	args := []string{argWorkspace, workspace}
	if resumeID != "" {
		args = append(args, argResume, resumeID)
	}
	return args
}

// execArgs returns the argv tail used by headless runs:
// `exec --model <m> --auto -- <prompt>`. The literal `--` pins the
// prompt as a positional so a leading `-` / `--` line in the user's
// spec body is not parsed as a flag (mirrors the claude backend's
// guard against the same regression).
func execArgs(model, prompt string) []string {
	return []string{argExec, argModel, model, argAuto, "--", prompt}
}
