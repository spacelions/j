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
// many times each prompt was invoked.
type scriptedUI struct {
	target   string
	tool     string
	model    string
	askErr   error
	toolErr  error
	modelErr error

	askCalls   int
	toolCalls  int
	modelCalls int
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

func (s *scriptedAgent) Plan(_ context.Context, req codingagents.PlanRequest) (string, error) {
	s.planned++
	s.lastReq = req
	if s.planErr != nil {
		return "", s.planErr
	}
	return s.plan, nil
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
		Target: target,
		Stdin:  strings.NewReader(""),
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
	if ui.toolCalls != 1 || ui.modelCalls != 1 {
		t.Fatalf("tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if agent.listed != 1 || agent.checked != 1 || agent.planned != 1 {
		t.Fatalf("agent calls listed=%d checked=%d planned=%d", agent.listed, agent.checked, agent.planned)
	}
	if agent.lastReq.TargetPath != target || agent.lastReq.Model != "sonnet-4" {
		t.Fatalf("PlanRequest = %+v", agent.lastReq)
	}
	if !strings.Contains(agent.lastReq.Body, "# task") {
		t.Fatalf("body = %q", agent.lastReq.Body)
	}

	plan, err := os.ReadFile(filepath.Join(filepath.Dir(target), "plan.md"))
	if err != nil {
		t.Fatalf("plan.md: %v", err)
	}
	got := strings.TrimSpace(string(plan))
	if got != "1. step one\n2. step two" {
		t.Fatalf("plan body = %q", got)
	}
	if !strings.Contains(stdout.String(), "wrote ") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRun_PromptsForTarget_WhenFlagMissing(t *testing.T) {
	target := writeTarget(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{target: target}

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
	if ui.askCalls != 1 {
		t.Fatalf("AskTarget called %d times, want 1", ui.askCalls)
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

func TestRun_WriteError(t *testing.T) {
	target := writeTarget(t, "x")
	dir := filepath.Dir(target)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := Run(context.Background(), Options{
		Target: target,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "write") {
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
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(target), "plan.md")); statErr == nil {
		t.Fatal("plan.md should not have been written")
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
