package plan

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
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
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
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
// source (SourceMarkdown == 0) so existing tests that exercise the
// markdown flow keep working without explicit setup.
type scriptedUI struct {
	source    PlanSource
	fromFile  string
	tool      string
	model     string
	sourceErr error
	askErr    error
	toolErr   error
	modelErr  error

	sourceCalls int
	askCalls    int
	toolCalls   int
	modelCalls  int
}

func (s *scriptedUI) SelectSource(context.Context) (PlanSource, error) {
	s.sourceCalls++
	if s.sourceErr != nil {
		return 0, s.sourceErr
	}
	return s.source, nil
}

func (s *scriptedUI) AskFromFile(context.Context) (string, error) {
	s.askCalls++
	if s.askErr != nil {
		return "", s.askErr
	}
	return s.fromFile, nil
}

func (s *scriptedUI) SelectTool(_ context.Context, options []string) (string, error) {
	s.toolCalls++
	if s.toolErr != nil {
		return "", s.toolErr
	}
	if s.tool != "" {
		return s.tool, nil
	}
	return options[0], nil
}

func (s *scriptedUI) SelectModel(_ context.Context, options []string) (string, error) {
	s.modelCalls++
	if s.modelErr != nil {
		return "", s.modelErr
	}
	if s.model != "" {
		return s.model, nil
	}
	return options[0], nil
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
// orchestrator's "could not read" warnings.
func (s *scriptedAgent) Plan(_ context.Context, req codingagents.PlanRequest) error {
	s.planned++
	s.lastReq = req
	if s.planErr != nil {
		return s.planErr
	}
	if s.skipWrite {
		return nil
	}
	req.RequirementsOutputPath = filepath.Clean(req.RequirementsOutputPath)
	req.PlanOutputPath = filepath.Clean(req.PlanOutputPath)
	requirement := s.requirement
	if requirement == "" {
		requirement = req.Body
	}
	if err := os.WriteFile(req.RequirementsOutputPath, []byte(requirement), 0o644); err != nil {
		return err
	}
	return os.WriteFile(req.PlanOutputPath, []byte(s.plan+"\n"), 0o644)
}

// Work is unused by plan_test but required to satisfy the
// codingagents.Agent interface, which gained Work alongside Plan.
func (s *scriptedAgent) Work(context.Context, codingagents.WorkRequest) error {
	return errors.New("scriptedAgent: Work should not be called from plan tests")
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
		Interactive: boolPtr(true),
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if ui.askCalls != 0 {
		t.Fatalf("AskFromFile called %d times, want 0", ui.askCalls)
	}
	if ui.toolCalls != 1 || ui.modelCalls != 1 {
		t.Fatalf("tool=%d model=%d", ui.toolCalls, ui.modelCalls)
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
	if !strings.Contains(agent.lastReq.Body, "# task") {
		t.Fatalf("body = %q", agent.lastReq.Body)
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
	if !strings.Contains(stdout.String(), "plan recorded as task ") {
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
		Interactive: boolPtr(false),
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

func TestRun_PromptsForTarget_WhenFlagMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{source: SourceMarkdown, fromFile: target}

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
	if ui.askCalls != 1 {
		t.Fatalf("AskFromFile called %d times, want 1", ui.askCalls)
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
	ui := &scriptedUI{source: SourceLinear}
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
	if ui.askCalls != 0 || agent.listed != 0 {
		t.Fatal("nothing past SelectSource should have been touched")
	}
}

// TestRun_UnsupportedSource pins the default branch in Run so adding a
// new PlanSource constant without a switch arm fails loudly in tests.
func TestRun_UnsupportedSource(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	agent := newScriptedAgent()
	ui := &scriptedUI{source: PlanSource(99)}
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

func TestRun_AskFromFileError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	agent := newScriptedAgent()
	ui := &scriptedUI{askErr: errors.New("ask boom")}
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "ask boom") {
		t.Fatalf("err = %v", err)
	}
	if agent.listed != 0 {
		t.Fatal("agent should not be invoked when AskFromFile errored")
	}
}

func TestRun_SelectModelError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	ui := &scriptedUI{modelErr: errors.New("model boom")}
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
	if ui.modelCalls != 0 {
		t.Fatalf("SelectModel called despite list error: %d", ui.modelCalls)
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
		UI:       &scriptedUI{toolErr: ErrCancelled},
	})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
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
		UI:       &scriptedUI{tool: "codex"},
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
		Interactive: boolPtr(true),
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
// case for the user-cancel path through agentpick.Pick.
func TestRun_SelectionCancelled_DoesNotPersist(t *testing.T) {
	s := openTestStore(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{toolErr: ErrCancelled},
		Store:    s,
	})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
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
// best-effort branch: a closed store returns errors from Put, and the
// agent must still run.
func TestRun_StoreWriteError_WarnsAndContinues(t *testing.T) {
	s := openTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
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

// TestPersistPlannerSelection_NilStore exercises the early-return
// branch when no Store is configured (e.g. because withDefaults could
// not open one and the caller did not supply a fallback).
func TestPersistPlannerSelection_NilStore(t *testing.T) {
	var stderr bytes.Buffer
	persistPlannerSelection(Options{Stderr: &stderr}, "cursor", "sonnet-4")
	if stderr.Len() != 0 {
		t.Fatalf("stderr should stay empty, got %q", stderr.String())
	}
}

// TestRun_FromSettings_PopulatedStore_SkipsPrompts pins the
// happy-path of --from-settings=true: a populated planner bucket
// causes Run to look up the agent by stored name, run CheckLogin,
// and skip the tool/model prompts entirely. The bucket must keep
// only the original three keys (no "from_settings" key, no rewrite).
func TestRun_FromSettings_PopulatedStore_SkipsPrompts(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "gpt-5"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "interactive", "true"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		FromFile:     target,
		Interactive:  boolPtr(true),
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           ui,
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.toolCalls != 0 || ui.modelCalls != 0 {
		t.Fatalf("UI prompts should be skipped: tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if agent.listed != 0 {
		t.Fatalf("ListModels should not be called when reading from settings (got %d)", agent.listed)
	}
	if agent.checked != 1 {
		t.Fatalf("CheckLogin = %d, want 1", agent.checked)
	}
	if agent.lastReq.Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", agent.lastReq.Model)
	}
	entries, err := s.List(store.BucketPlanner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := make([]string, len(entries))
	for i, kv := range entries {
		got[i] = kv.Key
	}
	want := []string{"interactive", "model", "tool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("planner keys = %v, want %v", got, want)
	}
	if strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should not warn when store is populated: %q", stderr.String())
	}
}

// TestRun_FromSettings_EmptyStore_FallsBackToPrompt covers the
// first-run case: --from-settings=true is the default but an empty
// bucket forces the interactive Pick flow with a single stderr
// breadcrumb explaining why.
func TestRun_FromSettings_EmptyStore_FallsBackToPrompt(t *testing.T) {
	s := openTestStore(t)
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		FromFile:     target,
		Interactive:  boolPtr(true),
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           ui,
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.toolCalls != 1 || ui.modelCalls != 1 {
		t.Fatalf("UI should be prompted: tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should warn about fallback: %q", stderr.String())
	}
	if v, ok := mustGet(t, s, "tool"); !ok || v != "cursor" {
		t.Fatalf("planner.tool = %q (ok=%v), want cursor", v, ok)
	}
}

// TestRun_FromSettings_False_AlwaysPrompts asserts the explicit
// opt-out: even when the store is fully populated, FromSettings=false
// forces the prompt path so users can change their mind.
func TestRun_FromSettings_False_AlwaysPrompts(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		FromFile:     target,
		FromSettings: false,
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           ui,
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.toolCalls != 1 || ui.modelCalls != 1 {
		t.Fatalf("UI should be prompted: tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should not warn on explicit --from-settings=false: %q", stderr.String())
	}
}

// TestRun_FromSettings_LoginFailureSurfaces covers the
// CheckLogin-error branch on the FromStore path: the error must
// propagate and the agent must NOT plan.
func TestRun_FromSettings_LoginFailureSurfaces(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")

	err := Run(context.Background(), Options{
		FromFile:     target,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan should not run when CheckLogin fails on FromStore path")
	}
}

// TestRun_FromSettings_NonSentinelStoreError pins the branch where
// FromStore returns an error other than ErrNoStoredSelection (an
// unknown tool name in the bucket): Run propagates it without
// falling back to Pick, since that's a real misconfiguration.
func TestRun_FromSettings_NonSentinelStoreError(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketPlanner, "tool", "ghost"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}

	err := Run(context.Background(), Options{
		FromFile:     target,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           ui,
		Store:        s,
	})
	if err == nil || !strings.Contains(err.Error(), `unknown tool "ghost"`) {
		t.Fatalf("err = %v", err)
	}
	if ui.toolCalls != 0 {
		t.Fatal("Pick should not be invoked on non-sentinel error")
	}
}

// TestRun_FromSettings_EmptyStore_PromptsPick asserts AC #4
// (fallback): with FromSettings, an empty project store triggers the
// "Choose your favourite:" line then Pick. The .j layout is
// pre-created via mustInit (the new pre-flight contract: callers
// must run `j init` before plan).
func TestRun_FromSettings_EmptyStore_PromptsPick(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		FromFile:     target,
		FromSettings: true,
		Stdin:        strings.NewReader(""),
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        nil,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr = %q, want choose-your-favourite line", stderr.String())
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file not created: %v", err)
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

// TestRun_FromSettings_StoredInteractiveFalseOverridesDefault pins
// the bug fix on the planner side: stored interactive=false flows
// through to the agent request and the persisted row when no
// explicit pointer is supplied.
func TestRun_FromSettings_StoredInteractiveFalseOverridesDefault(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "interactive", "false"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile:     target,
		Interactive:  nil,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if agent.lastReq.Interactive {
		t.Fatalf("agent.lastReq.Interactive = true, want false (stored override): %+v", agent.lastReq)
	}
	if v, ok := mustGet(t, s, "interactive"); !ok || v != "false" {
		t.Fatalf("planner.interactive = %q (ok=%v), want false", v, ok)
	}
}

// TestRun_FromSettings_ExplicitInteractiveWins documents the
// explicit-beats-stored half on the planner side.
func TestRun_FromSettings_ExplicitInteractiveWins(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "interactive", "false"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile:     target,
		Interactive:  boolPtr(true),
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("agent.lastReq.Interactive = false, want true (explicit wins): %+v", agent.lastReq)
	}
}

// TestRun_FromSettings_StoredInteractiveUnparseable confirms a
// garbled bucket value collapses to "not set" with no warning.
func TestRun_FromSettings_StoredInteractiveUnparseable(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "interactive", "garbage"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		FromFile:     target,
		Interactive:  nil,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("agent.lastReq.Interactive = false, want true (default): %+v", agent.lastReq)
	}
	if strings.Contains(stderr.String(), "interactive") {
		t.Fatalf("stderr should not warn on unparseable interactive: %q", stderr.String())
	}
}

// TestRun_FromSettings_False_IgnoresStoredInteractive pins the
// FromSettings=false branch on the planner side: explicit value
// flows through, bucket is ignored.
func TestRun_FromSettings_False_IgnoresStoredInteractive(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "interactive", "false"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile:     target,
		Interactive:  boolPtr(true),
		FromSettings: false,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("agent.lastReq.Interactive = false, want true: %+v", agent.lastReq)
	}
}

// TestRun_FromSettings_NoInteractiveKey_DefaultTrue locks down the
// resolveInteractive default branch: a populated bucket without an
// `interactive` entry leaves the cobra default (true) intact.
func TestRun_FromSettings_NoInteractiveKey_DefaultTrue(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := writeFromFile(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		FromFile:     target,
		Interactive:  nil,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("agent.lastReq.Interactive = false, want true (default): %+v", agent.lastReq)
	}
}
