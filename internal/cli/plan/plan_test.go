package plan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/picker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
)

// openTestStore returns a fresh *store.Store rooted in t.TempDir() with
// the planner bucket pre-created. The DB closes automatically via
// t.Cleanup so individual tests don't need to track it.
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.EnsureBucket(store.BucketPlanner); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// mustInit lays down the .j layout in the current working directory.
// Tests that previously relied on lazy creation by Run / OpenDefault /
// EnsureTaskDir must call this helper after t.Chdir so the new
// pre-init contract is satisfied. The helper is idempotent so calling
// it twice (e.g. via openTestStore) is harmless.
func mustInit(t *testing.T) {
	t.Helper()
	testutil.Init(t)
}

func mustGet(t *testing.T, s *store.Store, key string) (string, bool) {
	t.Helper()
	v, ok, err := s.Get(store.BucketPlanner, key)
	if err != nil {
		t.Fatalf("Get %s: %v", key, err)
	}
	return v, ok
}

// testCursorChatID is the session id the fake `cursor-agent` on PATH
// prints for `create-chat` (see TestMain), matching what Run stores in
// Task.PlanResumeCursor for the Cursor backend.
const testCursorChatID = "00000000-0000-4000-8000-000000000001"

// TestMain chdir's the entire plan-package test binary into an
// ephemeral directory so any test that calls Run without an explicit
// Store doesn't pollute the source tree with a `.j/settings` file
// when withDefaults lazily opens the default DB. Tests that need
// hermetic per-test storage call t.Chdir on top of this. It also prepends
// a minimal `cursor-agent` stub that only implements `create-chat` so
// `cursor.CreateChatID` used by Run does not require a real install.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "plan-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	if err := os.Chdir(tmp); err != nil {
		panic(err)
	}
	stubDir, err := os.MkdirTemp(tmp, "cursor-path")
	if err != nil {
		panic(err)
	}
	stub := filepath.Join(stubDir, "cursor-agent")
	stubScript := `#!/bin/sh
if [ "$1" = "create-chat" ]; then
  echo "00000000-0000-4000-8000-000000000001"
  exit 0
fi
echo "cursor-agent test stub: unhandled argv" >&2
exit 1
`
	if err := os.WriteFile(stub, []byte(stubScript), 0o755); err != nil {
		panic(err)
	}
	if err := os.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// scriptedUI returns predetermined answers for each prompt and tracks how
// many times each prompt was invoked. The zero value picks the markdown
// source (picker.SourceMarkdown == 0) so existing tests that exercise the
// markdown flow keep working without explicit setup.
//
// pickedFile is the basename returned from PickFromFile. When empty the
// fake selects options[0] so single-entry pickers "just work" in tests
// that only care about the rest of the flow. _ captures
// the option slice the orchestrator offered so tests can assert the
// scan + filter contract end-to-end.
type scriptedUI struct {
	testutil.SelectorFake

	source             picker.Source
	pickedMarkdownPath string
	pickedID           string
	replanID           string
	sourceErr          error
	pickedMarkdownErr  error
	pickErr            error
	replanErr          error
	confirm            bool
	confirmErr         error

	sourceCalls         int
	pickedMarkdownCalls int
	pickCalls           int
	replanCalls         int
	confirmCalls        int

	pickedTasks   []store.Task
	replanTasks   []store.Task
	confirmCmd    string
	confirmTaskID string
	confirmStatus string
}

func (s *scriptedUI) SelectSource(_ context.Context, _ []picker.Source) (picker.Source, error) {
	s.sourceCalls++
	if s.sourceErr != nil {
		return "", s.sourceErr
	}
	return s.source, nil
}

func (s *scriptedUI) PickMarkdownInCwd(_ context.Context) (string, error) {
	s.pickedMarkdownCalls++
	if s.pickedMarkdownErr != nil {
		return "", s.pickedMarkdownErr
	}
	return s.pickedMarkdownPath, nil
}

// PickTask dispatches by title prefix so the same scripted UI can
// answer both flows: titles that contain "re-plan" honour replanID /
// replanErr; titles that contain "resume" honour pickedID / pickErr.
// Both branches use the (id, ok, err) contract: ok=false collapses
// the user-abort path and the "no selection programmed" case so
// leaving the matching id field empty signals cancel.
func (s *scriptedUI) PickTask(_ context.Context, title string, tasks []store.Task) (string, bool, error) {
	if strings.Contains(title, "re-plan") {
		s.replanCalls++
		s.replanTasks = tasks
		if s.replanErr != nil {
			return "", false, s.replanErr
		}
		if s.replanID == "" {
			return "", false, nil
		}
		return s.replanID, true, nil
	}
	s.pickCalls++
	s.pickedTasks = tasks
	if s.pickErr != nil {
		return "", false, s.pickErr
	}
	if s.pickedID == "" {
		return "", false, nil
	}
	return s.pickedID, true, nil
}

func (s *scriptedUI) ConfirmStatusOverride(_ context.Context, cmd, taskID, status string) (bool, error) {
	s.confirmCalls++
	s.confirmCmd = cmd
	s.confirmTaskID = taskID
	s.confirmStatus = status
	if s.confirmErr != nil {
		return false, s.confirmErr
	}
	return s.confirm, nil
}


// scriptedAgent stands in for any codingagents.Agent in tests.
type scriptedAgent struct {
	name        string
	models      []string
	modelsErr   error
	loginErr    error
	resumeID    string
	resumeErr   error
	plan        string
	requirement string
	planErr     error
	skipWrite   bool
	// planPID, when non-zero and planErr is nil, is returned from
	// Plan to simulate a fire-and-forget headless spawn. The
	// orchestrator records the value as the task row's
	// BackgroundPID and skips the synchronous file-read +
	// finishPlan path.
	planPID int
	// planHook, when non-nil, is invoked at the start of Plan
	// before any side effects so tests can assert invariants
	// (e.g. that no bbolt file lock is held) while the agent is
	// "running". A non-nil error short-circuits Plan.
	planHook func(req codingagents.PlanRequest) error

	listed     int
	checked    int
	planned    int
	resumeIDed int
	lastReq    codingagents.PlanRequest
}

func newScriptedAgent() *scriptedAgent {
	return &scriptedAgent{
		name:     "cursor",
		models:   []string{"sonnet-4", "gpt-5"},
		plan:     "1. step one\n2. step two",
		resumeID: testCursorChatID,
	}
}

func (s *scriptedAgent) Name() string { return s.name }

func (s *scriptedAgent) ListModels(context.Context) ([]string, error) {
	s.listed++
	if s.modelsErr != nil {
		return nil, s.modelsErr
	}
	return s.models, nil
}

func (s *scriptedAgent) CheckLogin(context.Context) error {
	s.checked++
	return s.loginErr
}

func (s *scriptedAgent) NewResumeID(context.Context) (string, error) {
	s.resumeIDed++
	if s.resumeErr != nil {
		return "", s.resumeErr
	}
	return s.resumeID, nil
}

// Plan simulates a real backend: on success it writes both
// req.RequirementsOutputPath and req.PlanOutputPath itself (the
// agent's responsibility under the new contract). Tests can opt out of
// the file-write side effect by setting skipWrite to exercise the
// orchestrator's "could not read" warnings. planHook, when set, runs
// first so callers can assert mid-flight invariants. When planPID is
// non-zero the agent returns it as the spawned background PID
// without writing any artifacts (mirroring a fire-and-forget run).
func (s *scriptedAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	s.planned++
	s.lastReq = req
	if s.planHook != nil {
		if err := s.planHook(req); err != nil {
			return 0, err
		}
	}
	if s.planErr != nil {
		return 0, s.planErr
	}
	if s.planPID != 0 {
		return s.planPID, nil
	}
	if s.skipWrite {
		return 0, nil
	}
	req.RequirementsOutputPath = filepath.Clean(req.RequirementsOutputPath)
	req.PlanOutputPath = filepath.Clean(req.PlanOutputPath)
	requirement := s.requirement
	if requirement == "" {
		// Mirror what a real agent would do: read the user's
		// markdown source from disk and use it as the
		// requirements summary. Tests no longer thread the body
		// through PlanRequest.
		if data, err := os.ReadFile(req.FromFilePath); err == nil {
			requirement = string(data)
		}
	}
	if err := os.WriteFile(req.RequirementsOutputPath, []byte(requirement), 0o644); err != nil {
		return 0, err
	}
	return 0, os.WriteFile(req.PlanOutputPath, []byte(s.plan+"\n"), 0o644)
}

// Work is unused by plan_test but required to satisfy the
// codingagents.Agent interface, which gained Work alongside Plan.
func (s *scriptedAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	return 0, errors.New("scriptedAgent: Work should not be called from plan tests")
}

// Verify is unused by plan_test but required to satisfy the
// codingagents.Agent interface, which gained Verify alongside Plan
// and Work for the planner / worker / verifier loop.
func (s *scriptedAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("scriptedAgent: Verify should not be called from plan tests")
}

func writeFromFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun_Success_WithFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "# task\nbody")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		FromFile:    target,
		Interactive: true,
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if ui.pickedMarkdownCalls != 0 {
		t.Fatalf("PickFromFile called %d times, want 0", ui.pickedMarkdownCalls)
	}
	if ui.ToolCalls != 1 || ui.ModelCalls != 1 {
		t.Fatalf("tool=%d model=%d", ui.ToolCalls, ui.ModelCalls)
	}
	if agent.listed != 1 || agent.checked != 1 || agent.planned != 1 {
		t.Fatalf("agent calls listed=%d checked=%d planned=%d", agent.listed, agent.checked, agent.planned)
	}
	if agent.lastReq.FromFilePath != target || agent.lastReq.Model != "sonnet-4" {
		t.Fatalf("PlanRequest = %+v", agent.lastReq)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("Interactive flag was not propagated: %+v", agent.lastReq)
	}
	if !strings.Contains(agent.lastReq.RequirementsOutputPath, "requirements.md") {
		t.Fatalf("RequirementsOutputPath = %q", agent.lastReq.RequirementsOutputPath)
	}
	if !strings.Contains(agent.lastReq.PlanOutputPath, "plan.md") {
		t.Fatalf("PlanOutputPath = %q", agent.lastReq.PlanOutputPath)
	}
	if filepath.Dir(agent.lastReq.RequirementsOutputPath) != filepath.Dir(agent.lastReq.PlanOutputPath) {
		t.Fatalf("requirements and plan paths in different dirs: %q vs %q",
			agent.lastReq.RequirementsOutputPath, agent.lastReq.PlanOutputPath)
	}
	if agent.lastReq.FromFilePath == "" {
		t.Fatalf("FromFilePath should be populated: %+v", agent.lastReq)
	}

	plan, err := os.ReadFile(agent.lastReq.PlanOutputPath)
	if err != nil {
		t.Fatalf("plan.md: %v", err)
	}
	got := strings.TrimSpace(string(plan))
	if got != "1. step one\n2. step two" {
		t.Fatalf("plan body = %q", got)
	}
	req, err := os.ReadFile(agent.lastReq.RequirementsOutputPath)
	if err != nil {
		t.Fatalf("requirements.md: %v", err)
	}
	if !strings.Contains(string(req), "# task") {
		t.Fatalf("requirements body = %q", req)
	}
	if !strings.Contains(stdout.String(), "the requirements.md and plan.md are saved in .j/tasks/") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRun_Headless_PropagatesFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		FromFile:    target,
		Interactive: false,
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if agent.lastReq.Interactive {
		t.Fatalf("Interactive should be false: %+v", agent.lastReq)
	}
}

// TestRun_AgentDidNotWriteFiles pins the warning path when the agent
// returns success but did not save either requirements.md or plan.md.
// Both warnings must surface and the task must still be recorded as
// plan-done (the task lifecycle treats agent error as the only fatal
// signal here).
func TestRun_AgentDidNotWriteFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	agent.skipWrite = true
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "requirements.md") {
		t.Fatalf("stderr should warn about requirements.md: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "plan.md") {
		t.Fatalf("stderr should warn about plan.md: %q", stderr.String())
	}
}

// TestRun_PromptsForTarget_WhenFlagMissing exercises the prompted
// markdown branch end-to-end: chdir into a temp dir, drop a single
// `spec.md`, and assert the picker received `["spec.md"]` and the
// chosen entry flowed all the way through to agent.Plan as the
// `FromFilePath`.
func TestRun_PromptsForTarget_WhenFlagMissing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInit(t)
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	ui := &scriptedUI{source: picker.SourceMarkdown, pickedMarkdownPath: target}

	err := Run(context.Background(), Options{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.sourceCalls != 1 {
		t.Fatalf("SelectSource called %d times, want 1", ui.sourceCalls)
	}
	if ui.pickedMarkdownCalls != 1 {
		t.Fatalf("PickMarkdownInCwd called %d times, want 1", ui.pickedMarkdownCalls)
	}
	if agent.lastReq.FromFilePath != target {
		t.Fatalf("FromFilePath = %q, want %q", agent.lastReq.FromFilePath, target)
	}
}

// TestRun_FromFileFlag_BypassesSourceSelector pins the rule that an
// explicit -f / PLAN_FROM_FILE takes the markdown path without
// prompting the user for a source.
func TestRun_FromFileFlag_BypassesSourceSelector(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.sourceCalls != 0 {
		t.Fatalf("SelectSource called %d times, want 0", ui.sourceCalls)
	}
}

func TestRun_Linear_NoOp(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	agent := newScriptedAgent()
	ui := &scriptedUI{source: picker.SourceLinear}
	var stdout bytes.Buffer
	err := Run(context.Background(), Options{
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if agent.listed != 0 || agent.checked != 0 || agent.planned != 0 {
		t.Fatalf("linear should not touch the agent: listed=%d checked=%d planned=%d", agent.listed, agent.checked, agent.planned)
	}
	if !strings.Contains(stdout.String(), "linear") {
		t.Fatalf("stdout should mention linear: %q", stdout.String())
	}
}

func TestRun_SelectSourceError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	agent := newScriptedAgent()
	ui := &scriptedUI{sourceErr: errors.New("source boom")}
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "source boom") {
		t.Fatalf("err = %v", err)
	}
	if ui.pickedMarkdownCalls != 0 || agent.listed != 0 {
		t.Fatal("nothing past SelectSource should have been touched")
	}
}

// TestRun_UnsupportedSource pins the default branch in Run so adding a
// new picker.Source constant without a switch arm fails loudly in tests.
func TestRun_UnsupportedSource(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	agent := newScriptedAgent()
	ui := &scriptedUI{source: picker.Source("garbage")}
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported source") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_TargetValidationErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "spec.txt")
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		FromFile: bad,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "not a markdown") {
		t.Fatalf("err = %v", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan should not have been invoked")
	}
}

func TestRun_NoAgents(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		UI:       &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected error when no agents are configured")
	}
}

// TestRun_NoAgents_AppliesDefaults exercises the nil-defaulting branches
// in Options.withDefaults by passing a fully zero Options and relying on
// Run to short-circuit on the empty agent list before any UI is touched.
func TestRun_NoAgents_AppliesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	err := Run(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_PickFromFileError pins the picker error path: a UI that
// returns from PickFromFile must propagate the error verbatim and
// must not invoke any agent. The cwd has a single eligible markdown
// file so the orchestrator does reach the picker (and thus the
// scripted error) instead of short-circuiting on an empty scan.
func TestRun_PickFromFileError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInit(t)
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	ui := &scriptedUI{source: picker.SourceMarkdown, pickedMarkdownErr: errors.New("pick boom")}
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "pick boom") {
		t.Fatalf("err = %v", err)
	}
	if agent.listed != 0 {
		t.Fatal("agent should not be invoked when PickFromFile errored")
	}
}

// TestRun_MarkdownPicker_FiltersAndExcludes drives the picker
// end-to-end with a realistic cwd: two eligible markdown files, two
// excluded basenames (AGENTS.md / readme.MD), one non-markdown
// (`draft.txt`), and a subdirectory the scanner must skip. The
// orchestrator must offer exactly the two eligible basenames in
// case-insensitive sorted order, and the chosen entry must flow all
// the way through to agent.Plan as the absolute path of `spec.md`.
func TestRun_MarkdownPicker_FiltersAndExcludes(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInit(t)
	for _, name := range []string{
		"spec.md",
		"notes.markdown",
		"AGENTS.md",
		"readme.MD",
		"draft.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("body"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subdir", "nested.md"), []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}

	agent := newScriptedAgent()
	wantTarget := filepath.Join(dir, "spec.md")
	ui := &scriptedUI{source: picker.SourceMarkdown, pickedMarkdownPath: wantTarget}

	err := Run(context.Background(), Options{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Picker owns the filtering of cwd's markdown files (mdfile.ListInDir
	// behaviour is exercised in mdfile_test.go). The cli-level
	// invariant we pin here is that the picker's chosen path flows
	// through to agent.Plan as the absolute FromFilePath.
	if agent.lastReq.FromFilePath != wantTarget {
		t.Fatalf("FromFilePath = %q, want %q", agent.lastReq.FromFilePath, wantTarget)
	}
	if agent.planned != 1 {
		t.Fatalf("agent.planned = %d, want 1", agent.planned)
	}
}

// Markdown-picker error / scan / empty-dir / unknown-selection tests
// moved to internal/cli/picker (see picker_test.go::
// TestPickMarkdownInCwd_NoFiles and friends): those branches now
// live inside picker.PickMarkdownInCwd, exercised through the picker
// package's own unit tests rather than from the cli boundary.
// The cli-level invariants — that a picker error short-circuits Run
// without invoking the agent, and that the picker's chosen path
// flows into agent.Plan as FromFilePath — are pinned by
// TestRun_PickFromFileError + TestRun_MarkdownPicker_FiltersAndExcludes.

func TestRun_SelectModelError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	ui := &scriptedUI{SelectorFake: testutil.SelectorFake{ModelErr: errors.New("model boom")}}
	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       ui,
	})
	if err == nil || !strings.Contains(err.Error(), "model boom") {
		t.Fatalf("err = %v", err)
	}
	if agent.checked != 0 {
		t.Fatal("CheckLogin should not be invoked when SelectModel errored")
	}
}

func TestRun_TargetReadError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	if err := os.Chmod(target, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o600) })

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		UI:       &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "read source") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_ListModelsError_StopsBeforeUI(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	agent.modelsErr = errors.New("network down")

	ui := &scriptedUI{}
	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       ui,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if ui.ModelCalls != 0 {
		t.Fatalf("SelectModel called despite list error: %d", ui.ModelCalls)
	}
	if agent.checked != 0 || agent.planned != 0 {
		t.Fatal("login/plan should not have been invoked")
	}
}

func TestRun_LoginFailure_StopsBeforeAgent(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan should not have been invoked")
	}
}

// TestRun_UICancelled exercises the user-abort path: when a huh
// prompt returns huh.ErrUserAborted, Run treats it as a clean exit
// (nil error) and never reaches the agent.
func TestRun_UICancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{SelectorFake: testutil.SelectorFake{ToolErr: huh.ErrUserAborted}},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil (abort exits cleanly)", err)
	}
	if agent.listed != 0 || agent.planned != 0 {
		t.Fatal("agent should not be touched after cancel")
	}
}

func TestRun_AgentPlanError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	agent.planErr = errors.New("agent boom")

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRun_NewResumeID_ErrorWarnsButContinues(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	agent.resumeErr = errors.New("create-chat down")
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "create-chat down") {
		t.Fatalf("stderr should warn about resume-id failure: %q", stderr.String())
	}
	if agent.lastReq.ResumeChatID != "" {
		t.Fatalf("ResumeChatID should be empty after NewResumeID error: %q", agent.lastReq.ResumeChatID)
	}
}

func TestRun_UnknownToolFromUI(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	agent.name = "cursor"

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{SelectorFake: testutil.SelectorFake{Tool: "codex"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_Markdown_PersistsPlannerSelection drives a successful
// markdown run with a real *store.Store and asserts the planner
// bucket holds tool/model/interactive only — source and target must
// stay unpersisted so the user is prompted for them every run.
func TestRun_Markdown_PersistsPlannerSelection(t *testing.T) {
	s := openTestStore(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile:    target,
		Interactive: true,
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          &scriptedUI{},
		Store:       s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := map[string]string{
		"tool":        "cursor",
		"model":       "sonnet-4",
		"interactive": "true",
	}
	for k, v := range want {
		got, ok := mustGet(t, s, k)
		if !ok {
			t.Fatalf("missing key %q", k)
		}
		if got != v {
			t.Fatalf("planner.%s = %q, want %q", k, got, v)
		}
	}
	for _, forbidden := range []string{"source", "target", "from_file"} {
		if _, ok := mustGet(t, s, forbidden); ok {
			t.Fatalf("planner.%s should not be persisted", forbidden)
		}
	}
}

// TestRun_LoginFailure_DoesNotPersist confirms the planner bucket is
// untouched when login fails (we only persist after pickAgentAndModel
// returns successfully).
func TestRun_LoginFailure_DoesNotPersist(t *testing.T) {
	s := openTestStore(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
		Store:    s,
	})
	if err == nil {
		t.Fatal("expected login error")
	}
	entries, listErr := s.List(store.BucketPlanner)
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(entries) != 0 {
		t.Fatalf("planner bucket should be empty: %v", entries)
	}
}

// TestRun_SelectionCancelled_DoesNotPersist mirrors the login-failure
// case for the user-cancel path through picker.PickAgent. With the
// abort-to-nil contract, Run returns no error on cancel; the
// invariant the test guards is that nothing was persisted to the
// planner bucket because Pick was never confirmed.
func TestRun_SelectionCancelled_DoesNotPersist(t *testing.T) {
	s := openTestStore(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{SelectorFake: testutil.SelectorFake{ToolErr: huh.ErrUserAborted}},
		Store:    s,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil (abort exits cleanly)", err)
	}
	entries, listErr := s.List(store.BucketPlanner)
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(entries) != 0 {
		t.Fatalf("planner bucket should be empty after cancel: %v", entries)
	}
}

// TestRun_StoreWriteError_WarnsAndContinues exercises the persistence
// best-effort branch: an empty bucket sends Run through the Pick
// path, and a tool-hook closes the store mid-Pick so the post-Pick
// Put fails. The agent must still run and stderr must carry the
// "warning: persist" line.
func TestRun_StoreWriteError_WarnsAndContinues(t *testing.T) {
	s := openTestStore(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	var stderr bytes.Buffer
	ui := &scriptedUI{SelectorFake: testutil.SelectorFake{ToolHook: func() { _ = s.Close() }}}

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		UI:       ui,
		Store:    s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: persist") {
		t.Fatalf("stderr = %q, want warning", stderr.String())
	}
	if agent.planned != 1 {
		t.Fatal("agent.Plan should still have been invoked despite persist error")
	}
}

// TestRun_StoreReadError_Surfaces pins the new contract for a
// broken settings DB: when reading the planner bucket fails for a
// non-sentinel reason (e.g. the bbolt file is closed), Run aborts
// before invoking the agent. Users must see the error rather than
// silently losing their stored selection.
func TestRun_StoreReadError_Surfaces(t *testing.T) {
	s := openTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
		Store:    s,
	})
	if err == nil || !strings.Contains(err.Error(), "resolver: read planner") {
		t.Fatalf("err = %v, want wrapped read error", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan must not run when settings DB is broken")
	}
}

// TestRun_ExplicitTool_SkipsPersistence asserts the new --tool /
// --model contract: when both flags are supplied, Run resolves via
// resolver.Agent, runs the chosen agent, and leaves the planner
// bucket untouched (no UI prompt and no store write).
func TestRun_ExplicitTool_SkipsPersistence(t *testing.T) {
	s := openTestStore(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}

	err := Run(context.Background(), Options{
		FromFile: target,
		Tool:     "cursor",
		Model:    "opus",
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       ui,
		Store:    s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.ToolCalls != 0 || ui.ModelCalls != 0 {
		t.Fatalf("UI prompts should be skipped: tool=%d model=%d", ui.ToolCalls, ui.ModelCalls)
	}
	if agent.lastReq.Model != "opus" {
		t.Fatalf("model = %q, want opus", agent.lastReq.Model)
	}
	entries, err := s.List(store.BucketPlanner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("planner bucket should be untouched, got %d entries", len(entries))
	}
}

// TestRun_ExplicitTool_NilStore_LazyOpenSucceeds drives the
// nil-Store branch of resolver.Agent (explicit branch). The lazy open finds
// the seeded planner.model so --tool=cursor resolves cleanly.
func TestRun_ExplicitTool_NilStore_LazyOpenSucceeds(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketPlanner, "model", "stored-model"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	err = Run(context.Background(), Options{
		FromFile: target,
		Tool:     "cursor",
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if agent.lastReq.Model != "stored-model" {
		t.Fatalf("model = %q, want stored-model (lazy-open path)", agent.lastReq.Model)
	}
}

// TestRun_ExplicitTool_NilStore_LazyOpenFails covers the
// settings-DB-broken branch of resolver.Agent (explicit branch): with no
// stored half available, Resolve surfaces the missing-half error
// before invoking the agent.
func TestRun_ExplicitTool_NilStore_LazyOpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	settingsPath, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settingsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	var stderr bytes.Buffer
	err = Run(context.Background(), Options{
		FromFile: target,
		Tool:     "cursor",
		Stdout:   io.Discard,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "given without stored model in planner") {
		t.Fatalf("err = %v, want missing-model error", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan must not run when settings DB is broken")
	}
}

// TestRun_PartialModel_NoStoredTool errors before invoking the agent.
func TestRun_PartialModel_NoStoredTool(t *testing.T) {
	s := openTestStore(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile: target,
		Model:    "opus",
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
		Store:    s,
	})
	if err == nil || !strings.Contains(err.Error(), "given without stored tool in planner") {
		t.Fatalf("err = %v, want missing-tool error", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan should not run when explicit resolve fails")
	}
}

// TestRun_StoreLazyDefault confirms that a nil opts.Store causes
// withDefaults to open the existing default DB created by mustInit.
// The pre-flight contract guarantees the file is on disk before Run.
func TestRun_StoreLazyDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("default DB unexpectedly missing: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	got, ok, err := s.Get(store.BucketPlanner, "tool")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || got != "cursor" {
		t.Fatalf("planner.tool = %q (ok=%v)", got, ok)
	}
}

// TestRun_EnsureTaskDirError pins the legacy-file branch in
// runMarkdown: a regular file at .j/tasks blocks creating
// .j/tasks/<id>/, so plan must surface a clean "ensure task dir"
// error before the agent is invoked.
func TestRun_EnsureTaskDirError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jdir, "tasks"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "ensure task dir") {
		t.Fatalf("err = %v, want ensure-task-dir error", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan should not run when EnsureTaskDir fails")
	}
}

// TestRun_BackgroundSpawn_RecordsPID exercises the fire-and-forget
// headless path: the scripted agent returns a positive PID, the
// orchestrator must record it on the task row alongside the agent
// log path, status must stay `planning` (the row will only flip to
// plan-done once `j tasks` reaps the dead PID), no plan_end_at is
// stamped, and stdout carries the user-facing background message.
func TestRun_BackgroundSpawn_RecordsPID(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "# spec\nbody")
	agent := newScriptedAgent()
	agent.planPID = 42424
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		FromFile:    target,
		Interactive: false,
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout.String(), "running in background") {
		t.Fatalf("stdout = %q, want background message", stdout.String())
	}
	if !strings.Contains(stdout.String(), "PID=42424") {
		t.Fatalf("stdout = %q, want PID=42424", stdout.String())
	}
	if !strings.Contains(stdout.String(), "J: "+agent.Name()+" running in background") {
		t.Fatalf("stdout = %q, want agent name %q", stdout.String(), agent.Name())
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}
	got := tasks[0]
	if got.Status != store.StatusPlanning {
		t.Fatalf("Status = %q, want planning", got.Status)
	}
	if got.BackgroundPID != 42424 {
		t.Fatalf("BackgroundPID = %d, want 42424", got.BackgroundPID)
	}
	if got.AgentLogPath == "" || filepath.Base(got.AgentLogPath) != "agent.log" {
		t.Fatalf("AgentLogPath = %q, want path ending in agent.log", got.AgentLogPath)
	}
	if got.PlanEndAt != nil {
		t.Fatalf("PlanEndAt should remain nil for background row: %v", got.PlanEndAt)
	}
	// AgentLogPath was passed through to the agent.
	if agent.lastReq.AgentLogPath != got.AgentLogPath {
		t.Fatalf("AgentLogPath flowed wrong: req=%q row=%q",
			agent.lastReq.AgentLogPath, got.AgentLogPath)
	}
}

// TestRun_DoesNotHoldFileLocks_DuringAgentPlan is the regression
// guard for the open-write-close refactor: while agent.Plan is
// running, both `<cwd>/.j/settings` and `<cwd>/.j/tasks/list.db`
// must be openable by another caller without hitting the bbolt
// 2-second openTimeout. The scripted agent's planHook performs the
// concurrent opens and short-circuits Plan with an error when either
// open fails, which propagates back through Run as a real
// agent.Plan error.
func TestRun_DoesNotHoldFileLocks_DuringAgentPlan(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "# spec\nbody")

	settingsPath, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	tasksPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}

	agent := newScriptedAgent()
	agent.planHook = func(_ codingagents.PlanRequest) error {
		s, err := store.Open(settingsPath)
		if err != nil {
			return fmt.Errorf("settings db should not be locked: %w", err)
		}
		if err := s.Close(); err != nil {
			return fmt.Errorf("close settings: %w", err)
		}
		s, err = store.Open(tasksPath)
		if err != nil {
			return fmt.Errorf("tasks db should not be locked: %w", err)
		}
		if err := s.Close(); err != nil {
			return fmt.Errorf("close tasks: %w", err)
		}
		return nil
	}

	err = Run(context.Background(), Options{
		FromFile:    target,
		Interactive: true,
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v (a non-nil err here means a bbolt lock was held across agent.Plan)", err)
	}
	if agent.planned != 1 {
		t.Fatalf("agent.Plan calls = %d, want 1", agent.planned)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusPlanDone {
		t.Fatalf("tasks = %+v, want one plan-done task", tasks)
	}
}
