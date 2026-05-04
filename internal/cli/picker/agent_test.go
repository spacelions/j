package picker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// scriptedUI is the in-package fake for Selector. Every field is
// optional; the zero value picks the first option for both prompts.
type scriptedUI struct {
	tool     string
	model    string
	toolErr  error
	modelErr error

	toolCalls  int
	modelCalls int
	lastTools  []string
	lastModels []string
}

func (s *scriptedUI) SelectTool(_ context.Context, options []string) (string, error) {
	s.toolCalls++
	s.lastTools = append([]string(nil), options...)
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
	s.lastModels = append([]string(nil), options...)
	if s.modelErr != nil {
		return "", s.modelErr
	}
	if s.model != "" {
		return s.model, nil
	}
	return options[0], nil
}

// stubAgent is the in-package fake for codingagents.Agent. Plan and
// Work return errors so accidental invocation in this package's tests
// is loud — Pick must not call either.
type stubAgent struct {
	name      string
	models    []string
	modelsErr error
	loginErr  error

	listed  int
	checked int
}

func newStubAgent(name string, models ...string) *stubAgent {
	return &stubAgent{name: name, models: models}
}

func (s *stubAgent) Name() string { return s.name }

func (s *stubAgent) ListModels(context.Context) ([]string, error) {
	s.listed++
	if s.modelsErr != nil {
		return nil, s.modelsErr
	}
	return s.models, nil
}

func (s *stubAgent) CheckLogin(context.Context) error {
	s.checked++
	return s.loginErr
}

func (s *stubAgent) NewResumeID(context.Context) (string, error) {
	return "", errors.New("picker: NewResumeID should not be called")
}

func (s *stubAgent) Plan(context.Context, codingagents.PlanRequest) (int, error) {
	return 0, errors.New("picker: Plan should not be called")
}

func (s *stubAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	return 0, errors.New("picker: Work should not be called")
}

func (s *stubAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("picker: Verify should not be called")
}

func TestPick_Success(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4", "gpt-5")
	codex := newStubAgent("codex", "o4")
	ui := &scriptedUI{tool: "cursor", model: "gpt-5"}

	agent, model, err := PickAgent(context.Background(), ui, []codingagents.Agent{cursor, codex})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if agent != cursor {
		t.Fatalf("agent = %v, want cursor", agent.Name())
	}
	if model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", model)
	}

	if !reflect.DeepEqual(ui.lastTools, []string{"cursor", "codex"}) {
		t.Fatalf("SelectTool got options %v", ui.lastTools)
	}
	if !reflect.DeepEqual(ui.lastModels, []string{"sonnet-4", "gpt-5"}) {
		t.Fatalf("SelectModel got options %v", ui.lastModels)
	}
	if cursor.listed != 1 || cursor.checked != 1 {
		t.Fatalf("cursor calls: listed=%d checked=%d", cursor.listed, cursor.checked)
	}
	if codex.listed != 0 || codex.checked != 0 {
		t.Fatalf("codex should be untouched: listed=%d checked=%d", codex.listed, codex.checked)
	}
}

func TestPick_SelectToolError(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4")
	ui := &scriptedUI{toolErr: errors.New("tool boom")}

	_, _, err := PickAgent(context.Background(), ui, []codingagents.Agent{cursor})
	if err == nil || !strings.Contains(err.Error(), "tool boom") {
		t.Fatalf("err = %v", err)
	}
	if cursor.listed != 0 || cursor.checked != 0 {
		t.Fatalf("agent should be untouched: listed=%d checked=%d", cursor.listed, cursor.checked)
	}
}

func TestPick_UnknownTool(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4")
	ui := &scriptedUI{tool: "ghost"}

	_, _, err := PickAgent(context.Background(), ui, []codingagents.Agent{cursor})
	if err == nil || !strings.Contains(err.Error(), `unknown tool "ghost"`) {
		t.Fatalf("err = %v", err)
	}
	if cursor.listed != 0 {
		t.Fatal("ListModels should not be called when lookup fails")
	}
}

func TestPick_ListModelsError(t *testing.T) {
	cursor := newStubAgent("cursor")
	cursor.modelsErr = errors.New("list boom")
	ui := &scriptedUI{}

	_, _, err := PickAgent(context.Background(), ui, []codingagents.Agent{cursor})
	if err == nil || !strings.Contains(err.Error(), "list boom") {
		t.Fatalf("err = %v", err)
	}
	if ui.modelCalls != 0 {
		t.Fatal("SelectModel should not be called when ListModels fails")
	}
	if cursor.checked != 0 {
		t.Fatal("CheckLogin should not be called when ListModels fails")
	}
}

func TestPick_SelectModelError(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4")
	ui := &scriptedUI{modelErr: errors.New("model boom")}

	_, _, err := PickAgent(context.Background(), ui, []codingagents.Agent{cursor})
	if err == nil || !strings.Contains(err.Error(), "model boom") {
		t.Fatalf("err = %v", err)
	}
	if cursor.checked != 0 {
		t.Fatal("CheckLogin should not be called when SelectModel fails")
	}
}

func TestPick_CheckLoginError(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4")
	cursor.loginErr = errors.New("not logged in")
	ui := &scriptedUI{}

	_, _, err := PickAgent(context.Background(), ui, []codingagents.Agent{cursor})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
	if cursor.checked != 1 {
		t.Fatalf("CheckLogin called %d times, want 1", cursor.checked)
	}
}

// TestPick_NoAgents pins the empty-slice behavior: Pick still calls
// SelectTool with a zero-length list and the UI is responsible for
// surfacing "no options". Callers (plan.Run, work.Run) guard against
// an empty Agents slice before invoking Pick, so this is a defensive
// contract for code that bypasses the guard.
func TestPick_NoAgents(t *testing.T) {
	ui := &scriptedUI{toolErr: errors.New("no options")}
	_, _, err := PickAgent(context.Background(), ui, nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v", err)
	}
	if ui.toolCalls != 1 {
		t.Fatalf("SelectTool called %d times, want 1", ui.toolCalls)
	}
	if len(ui.lastTools) != 0 {
		t.Fatalf("lastTools = %v, want empty", ui.lastTools)
	}
}
