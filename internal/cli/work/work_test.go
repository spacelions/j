package work

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

// scriptedUI returns predetermined answers for each prompt and tracks
// how many times each prompt was invoked.
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

// scriptedAgent stands in for any codingagents.Agent in tests. Plan is
// implemented because the Agent interface requires it; work_test never
// invokes it.
type scriptedAgent struct {
	name      string
	models    []string
	modelsErr error
	loginErr  error
	workErr   error

	listed  int
	checked int
	worked  int
	lastReq codingagents.WorkRequest
}

func newScriptedAgent() *scriptedAgent {
	return &scriptedAgent{
		name:   "cursor",
		models: []string{"sonnet-4", "gpt-5"},
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

func (s *scriptedAgent) Plan(context.Context, codingagents.PlanRequest) error {
	return errors.New("scriptedAgent: Plan should not be called from work tests")
}

func (s *scriptedAgent) Work(_ context.Context, req codingagents.WorkRequest) error {
	s.worked++
	s.lastReq = req
	return s.workErr
}

func writePlan(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun_Success_WithFlag(t *testing.T) {
	plan := writePlan(t, "1. step one\n2. step two")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Target:      plan,
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
	if agent.listed != 1 || agent.checked != 1 || agent.worked != 1 {
		t.Fatalf("agent listed=%d checked=%d worked=%d", agent.listed, agent.checked, agent.worked)
	}
	if agent.lastReq.PlanPath != plan || agent.lastReq.Model != "sonnet-4" {
		t.Fatalf("WorkRequest = %+v", agent.lastReq)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("Interactive flag was not propagated: %+v", agent.lastReq)
	}
	if !strings.Contains(agent.lastReq.Body, "1. step one") {
		t.Fatalf("body = %q", agent.lastReq.Body)
	}
	if !strings.Contains(stdout.String(), "coding against ") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRun_Headless_PropagatesFlag(t *testing.T) {
	plan := writePlan(t, "x")
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		Target:      plan,
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

func TestRun_PromptsForTarget_WhenFlagMissing(t *testing.T) {
	plan := writePlan(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{target: plan}

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
	if agent.worked != 1 {
		t.Fatalf("agent.Work called %d times, want 1", agent.worked)
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
	if !strings.Contains(err.Error(), "not a plan markdown") {
		t.Fatalf("err = %v", err)
	}
	if agent.worked != 0 {
		t.Fatal("agent.Work should not have been invoked")
	}
}

func TestRun_PlanReadError(t *testing.T) {
	plan := writePlan(t, "x")
	if err := os.Chmod(plan, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(plan, 0o600) })

	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "read plan") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_NoAgents(t *testing.T) {
	plan := writePlan(t, "x")
	err := Run(context.Background(), Options{
		Target: plan,
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

func TestRun_ListModelsError_StopsBeforeUI(t *testing.T) {
	plan := writePlan(t, "x")
	agent := newScriptedAgent()
	agent.modelsErr = errors.New("network down")

	ui := &scriptedUI{}
	err := Run(context.Background(), Options{
		Target: plan,
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
	if agent.checked != 0 || agent.worked != 0 {
		t.Fatal("login/work should not have been invoked")
	}
}

func TestRun_SelectModelError(t *testing.T) {
	plan := writePlan(t, "x")
	agent := newScriptedAgent()
	ui := &scriptedUI{modelErr: errors.New("model boom")}
	err := Run(context.Background(), Options{
		Target: plan,
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

func TestRun_LoginFailure_StopsBeforeAgent(t *testing.T) {
	plan := writePlan(t, "x")
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")

	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
	if agent.worked != 0 {
		t.Fatal("agent.Work should not have been invoked")
	}
}

func TestRun_UICancelled(t *testing.T) {
	plan := writePlan(t, "x")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{toolErr: ErrCancelled},
	})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	if agent.listed != 0 || agent.worked != 0 {
		t.Fatal("agent should not be touched after cancel")
	}
}

func TestRun_AgentWorkError(t *testing.T) {
	plan := writePlan(t, "x")
	agent := newScriptedAgent()
	agent.workErr = errors.New("agent boom")

	var stdout bytes.Buffer
	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "agent boom") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stdout.String(), "coding against ") {
		t.Fatalf("stdout should not announce success on Work error: %q", stdout.String())
	}
}

func TestRun_UnknownToolFromUI(t *testing.T) {
	plan := writePlan(t, "x")
	agent := newScriptedAgent()
	agent.name = "cursor"

	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{tool: "codex"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("err = %v", err)
	}
}
