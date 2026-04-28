package work

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

// TestMain chdir's the entire work-package test binary into an
// ephemeral directory so any test that calls Run without an explicit
// Store doesn't pollute the source tree with a `.j/settings` file
// when withDefaults lazily opens the default DB.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "work-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	if err := os.Chdir(tmp); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// openTestStore returns a fresh *store.Store rooted in t.TempDir() with
// the coder bucket pre-created.
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	t.Chdir(t.TempDir())
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.EnsureBucket(store.BucketCoder); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustGet(t *testing.T, s *store.Store, key string) (string, bool) {
	t.Helper()
	v, ok, err := s.Get(store.BucketCoder, key)
	if err != nil {
		t.Fatalf("Get %s: %v", key, err)
	}
	return v, ok
}

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
	if !strings.Contains(err.Error(), "not a markdown") {
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

// TestRun_PersistsCoderSelection drives a successful work run with a
// real *store.Store and asserts the coder bucket holds tool/model/
// interactive only — the work source (plan path) must stay
// unpersisted so the user is prompted for it every run.
func TestRun_PersistsCoderSelection(t *testing.T) {
	s := openTestStore(t)
	plan := writePlan(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		Target:      plan,
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
		if !ok || got != v {
			t.Fatalf("coder.%s = %q (ok=%v), want %q", k, got, ok, v)
		}
	}
	for _, forbidden := range []string{"target", "source", "plan"} {
		if _, ok := mustGet(t, s, forbidden); ok {
			t.Fatalf("coder.%s should not be persisted", forbidden)
		}
	}
}

// TestRun_LoginFailure_DoesNotPersist confirms the coder bucket is
// untouched when login fails (we only persist after agentpick.Pick
// returns successfully).
func TestRun_LoginFailure_DoesNotPersist(t *testing.T) {
	s := openTestStore(t)
	plan := writePlan(t, "body")
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")

	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
		Store:  s,
	})
	if err == nil {
		t.Fatal("expected login error")
	}
	entries, listErr := s.List(store.BucketCoder)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(entries) != 0 {
		t.Fatalf("coder bucket should be empty: %v", entries)
	}
}

// TestRun_SelectionCancelled_DoesNotPersist mirrors the login-failure
// case for the user-cancel path through agentpick.Pick.
func TestRun_SelectionCancelled_DoesNotPersist(t *testing.T) {
	s := openTestStore(t)
	plan := writePlan(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{toolErr: ErrCancelled},
		Store:  s,
	})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v", err)
	}
	entries, listErr := s.List(store.BucketCoder)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(entries) != 0 {
		t.Fatalf("coder bucket should be empty: %v", entries)
	}
}

// TestRun_StoreWriteError_WarnsAndContinues exercises the persistence
// best-effort branch: a closed store returns errors from Put, and the
// agent must still run.
func TestRun_StoreWriteError_WarnsAndContinues(t *testing.T) {
	s := openTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	plan := writePlan(t, "body")
	agent := newScriptedAgent()
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
		Store:  s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: persist") {
		t.Fatalf("stderr = %q, want warning", stderr.String())
	}
	if agent.worked != 1 {
		t.Fatal("agent.Work should still have been invoked despite persist error")
	}
}

// TestRun_FromSettings_PopulatedStore_SkipsPrompts is the work
// counterpart of the plan test: a populated coder bucket must
// short-circuit the prompts and route the stored model into the
// WorkRequest. The bucket is left untouched (no "from_settings"
// key, no rewrite).
func TestRun_FromSettings_PopulatedStore_SkipsPrompts(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "gpt-5"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "interactive", "true"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	plan := writePlan(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		Target:       plan,
		Interactive:  true,
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
		t.Fatalf("ListModels should not be called: got %d", agent.listed)
	}
	if agent.checked != 1 {
		t.Fatalf("CheckLogin = %d, want 1", agent.checked)
	}
	if agent.lastReq.Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", agent.lastReq.Model)
	}
	entries, err := s.List(store.BucketCoder)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := make([]string, len(entries))
	for i, kv := range entries {
		got[i] = kv.Key
	}
	want := []string{"interactive", "model", "tool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("coder keys = %v, want %v", got, want)
	}
	if strings.Contains(stderr.String(), "no stored coder selection") {
		t.Fatalf("stderr should not warn when store is populated: %q", stderr.String())
	}
}

func TestRun_FromSettings_EmptyStore_FallsBackToPrompt(t *testing.T) {
	s := openTestStore(t)
	plan := writePlan(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		Target:       plan,
		Interactive:  true,
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
	if !strings.Contains(stderr.String(), "no stored coder selection; prompting") {
		t.Fatalf("stderr should warn about fallback: %q", stderr.String())
	}
	if v, ok := mustGet(t, s, "tool"); !ok || v != "cursor" {
		t.Fatalf("coder.tool = %q (ok=%v), want cursor", v, ok)
	}
}

func TestRun_FromSettings_False_AlwaysPrompts(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	plan := writePlan(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		Target:       plan,
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
	if strings.Contains(stderr.String(), "no stored coder selection") {
		t.Fatalf("stderr should not warn on explicit --from-settings=false: %q", stderr.String())
	}
}

func TestRun_FromSettings_LoginFailureSurfaces(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	plan := writePlan(t, "body")
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")

	err := Run(context.Background(), Options{
		Target:       plan,
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
	if agent.worked != 0 {
		t.Fatal("agent.Work should not run when CheckLogin fails on FromStore path")
	}
}

func TestRun_FromSettings_NonSentinelStoreError(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "ghost"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	plan := writePlan(t, "body")
	agent := newScriptedAgent()
	ui := &scriptedUI{}

	err := Run(context.Background(), Options{
		Target:       plan,
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

// TestRun_StoreLazyDefault confirms a nil opts.Store causes
// withDefaults to open and close the default DB and write to the
// coder bucket.
func TestRun_StoreLazyDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	plan := writePlan(t, "body")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		Target: plan,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	got, ok, err := s.Get(store.BucketCoder, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != "cursor" {
		t.Fatalf("coder.tool = %q (ok=%v)", got, ok)
	}
}

// TestPersistCoderSelection_NilStore exercises the early-return branch
// when no Store is configured.
func TestPersistCoderSelection_NilStore(t *testing.T) {
	var stderr bytes.Buffer
	persistCoderSelection(Options{Stderr: &stderr}, "cursor", "sonnet-4")
	if stderr.Len() != 0 {
		t.Fatalf("stderr should stay empty, got %q", stderr.String())
	}
}
