package plan

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// scriptedUI returns predetermined answers for each prompt and tracks how
// many times each prompt was invoked. The zero value picks the markdown
// source (SourceMarkdown == 0) so existing tests that exercise the
// markdown flow keep working without explicit setup.
type scriptedUI struct {
	source    PlanSource
	target    string
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

func (s *scriptedUI) AskTarget(context.Context) (string, error) {
	s.askCalls++
	if s.askErr != nil {
		return "", s.askErr
	}
	return s.target, nil
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
	name      string
	models    []string
	modelsErr error
	loginErr  error
	plan      string
	planErr   error
	skipWrite bool

	listed  int
	checked int
	planned int
	lastReq codingagents.PlanRequest
}

func newScriptedAgent() *scriptedAgent {
	return &scriptedAgent{
		name:   "cursor",
		models: []string{"sonnet-4", "gpt-5"},
		plan:   "1. step one\n2. step two",
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

// Plan simulates a real backend: on success it writes req.OutputPath
// itself (this is the agent's responsibility under the new contract,
// since cursor.Plan does so in both interactive and headless paths).
// Tests can opt out of the file-write side effect by setting skipWrite
// in order to exercise the orchestrator's "was not written" warning.
func (s *scriptedAgent) Plan(_ context.Context, req codingagents.PlanRequest) error {
	s.planned++
	s.lastReq = req
	if s.planErr != nil {
		return s.planErr
	}
	if s.skipWrite {
		return nil
	}
	return os.WriteFile(req.OutputPath, []byte(s.plan+"\n"), 0o644)
}

func writeTarget(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun_Success_WithFlag(t *testing.T) {
	target := writeTarget(t, "# task\nbody")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Target:      target,
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

	if ui.askCalls != 0 {
		t.Fatalf("AskTarget called %d times, want 0", ui.askCalls)
	}
	if ui.toolCalls != 1 || ui.modelCalls != 1 {
		t.Fatalf("tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if agent.listed != 1 || agent.checked != 1 || agent.planned != 1 {
		t.Fatalf("agent calls listed=%d checked=%d planned=%d", agent.listed, agent.checked, agent.planned)
	}
	if agent.lastReq.TargetPath != target || agent.lastReq.Model != "sonnet-4" {
		t.Fatalf("PlanRequest = %+v", agent.lastReq)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("Interactive flag was not propagated: %+v", agent.lastReq)
	}
	wantOut := filepath.Join(filepath.Dir(target), "spec.plan.md")
	if agent.lastReq.OutputPath != wantOut {
		t.Fatalf("OutputPath = %q, want %q", agent.lastReq.OutputPath, wantOut)
	}
	if !strings.Contains(agent.lastReq.Body, "# task") {
		t.Fatalf("body = %q", agent.lastReq.Body)
	}

	plan, err := os.ReadFile(wantOut)
	if err != nil {
		t.Fatalf("spec.plan.md: %v", err)
	}
	got := strings.TrimSpace(string(plan))
	if got != "1. step one\n2. step two" {
		t.Fatalf("plan body = %q", got)
	}
	if !strings.Contains(stdout.String(), "wrote ") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRun_Headless_PropagatesFlag(t *testing.T) {
	target := writeTarget(t, "x")
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		Target:      target,
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

func TestRun_AgentDidNotWriteFile(t *testing.T) {
	target := writeTarget(t, "x")
	agent := newScriptedAgent()
	agent.skipWrite = true
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "was not written") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRun_PromptsForTarget_WhenFlagMissing(t *testing.T) {
	target := writeTarget(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{source: SourceMarkdown, target: target}

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
		t.Fatalf("AskTarget called %d times, want 1", ui.askCalls)
	}
}

// TestRun_TargetFlag_BypassesSourceSelector pins the rule that an
// explicit -t / PLAN_TARGET takes the markdown path without prompting
// the user for a source.
func TestRun_TargetFlag_BypassesSourceSelector(t *testing.T) {
	target := writeTarget(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.sourceCalls != 0 {
		t.Fatalf("SelectSource called %d times, want 0", ui.sourceCalls)
	}
}

func TestRun_Scratch(t *testing.T) {
	agent := newScriptedAgent()
	agent.skipWrite = true // scratch never writes a file
	ui := &scriptedUI{source: SourceScratch}
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
	if ui.askCalls != 0 {
		t.Fatalf("AskTarget called %d times, want 0", ui.askCalls)
	}
	if agent.planned != 1 {
		t.Fatalf("agent.Plan called %d times, want 1", agent.planned)
	}
	if agent.lastReq.TargetPath != "" || agent.lastReq.Body != "" || agent.lastReq.OutputPath != "" {
		t.Fatalf("scratch req should be empty: %+v", agent.lastReq)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("scratch should always be interactive: %+v", agent.lastReq)
	}
	if strings.Contains(stdout.String(), "wrote ") {
		t.Fatalf("scratch should not announce a file: %q", stdout.String())
	}
}

func TestRun_Scratch_AgentError(t *testing.T) {
	agent := newScriptedAgent()
	agent.planErr = errors.New("scratch boom")
	ui := &scriptedUI{source: SourceScratch}
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "scratch boom") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_Scratch_LoginFails covers the pickAgentAndModel error branch
// in runScratch (via CheckLogin returning an error), which the happy
// path and Plan-error tests do not exercise.
func TestRun_Scratch_LoginFails(t *testing.T) {
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")
	ui := &scriptedUI{source: SourceScratch}
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan should not have been invoked")
	}
}

func TestRun_Linear_NoOp(t *testing.T) {
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
	dir := t.TempDir()
	bad := filepath.Join(dir, "spec.txt")
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		Target: bad,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
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
	target := writeTarget(t, "x")
	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected error when no agents are configured")
	}
}

// TestRun_NoAgents_AppliesDefaults exercises the nil-defaulting branches
// in Options.withDefaults by passing a fully zero Options and relying on
// Run to short-circuit on the empty agent list before any UI is touched.
func TestRun_NoAgents_AppliesDefaults(t *testing.T) {
	err := Run(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_AskTargetError(t *testing.T) {
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
		t.Fatal("agent should not be invoked when AskTarget errored")
	}
}

func TestRun_SelectModelError(t *testing.T) {
	target := writeTarget(t, "x")
	agent := newScriptedAgent()
	ui := &scriptedUI{modelErr: errors.New("model boom")}
	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "model boom") {
		t.Fatalf("err = %v", err)
	}
	if agent.checked != 0 {
		t.Fatal("CheckLogin should not be invoked when SelectModel errored")
	}
}

func TestRun_TargetReadError(t *testing.T) {
	target := writeTarget(t, "x")
	if err := os.Chmod(target, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o600) })

	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "read target") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_ListModelsError_StopsBeforeUI(t *testing.T) {
	target := writeTarget(t, "x")
	agent := newScriptedAgent()
	agent.modelsErr = errors.New("network down")

	ui := &scriptedUI{}
	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
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
	target := writeTarget(t, "x")
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")

	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
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
	target := writeTarget(t, "x")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{toolErr: ErrCancelled},
	})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	if agent.listed != 0 || agent.planned != 0 {
		t.Fatal("agent should not be touched after cancel")
	}
}

func TestRun_AgentPlanError(t *testing.T) {
	target := writeTarget(t, "x")
	agent := newScriptedAgent()
	agent.planErr = errors.New("agent boom")

	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(target), "spec.plan.md")); statErr == nil {
		t.Fatal("spec.plan.md should not have been written")
	}
}

func TestRun_UnknownToolFromUI(t *testing.T) {
	target := writeTarget(t, "x")
	agent := newScriptedAgent()
	agent.name = "cursor"

	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{tool: "codex"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("err = %v", err)
	}
}
