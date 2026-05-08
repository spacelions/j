package resolver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// openTestStore returns a fresh *store.Store with the named bucket
// pre-created, rooted at t.TempDir() so tests don't share state.
func openTestStore(t *testing.T, bucket string) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.EnsureBucket(bucket); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

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
	return "", errors.New("resolver: NewResumeID should not be called")
}

func (s *stubAgent) Plan(context.Context, codingagents.PlanRequest) (int, error) {
	return 0, errors.New("resolver: Plan should not be called")
}

func (s *stubAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	return 0, errors.New("resolver: Work should not be called")
}

func (s *stubAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("resolver: Verify should not be called")
}

func TestFromStore_NilStore(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4")
	_, _, err := AgentFromStore(t.Context(), nil, store.BucketPlanner, []codingagents.Agent{cursor})
	if !errors.Is(err, ErrNoStoredSelection) {
		t.Fatalf("err = %v, want ErrNoStoredSelection", err)
	}
	if cursor.checked != 0 {
		t.Fatal("CheckLogin should not run when store is nil")
	}
}

func TestFromStore_MissingTool(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	_, _, err := AgentFromStore(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor})
	if !errors.Is(err, ErrNoStoredSelection) {
		t.Fatalf("err = %v, want ErrNoStoredSelection", err)
	}
}

func TestFromStore_MissingModel(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	_, _, err := AgentFromStore(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor})
	if !errors.Is(err, ErrNoStoredSelection) {
		t.Fatalf("err = %v, want ErrNoStoredSelection", err)
	}
}

// TestFromStore_EmptyToolValue covers the rare case where the
// recorded value is an empty string. We treat it as "no selection"
// so first-run and corruption-recovery look identical to the caller.
func TestFromStore_EmptyToolValue(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "tool", ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	_, _, err := AgentFromStore(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor})
	if !errors.Is(err, ErrNoStoredSelection) {
		t.Fatalf("err = %v, want ErrNoStoredSelection", err)
	}
}

func TestFromStore_EmptyModelValue(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	_, _, err := AgentFromStore(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor})
	if !errors.Is(err, ErrNoStoredSelection) {
		t.Fatalf("err = %v, want ErrNoStoredSelection", err)
	}
}

func TestFromStore_UnknownTool(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "tool", "ghost"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	_, _, err := AgentFromStore(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor})
	if err == nil || !strings.Contains(err.Error(), `unknown tool "ghost"`) {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(err, ErrNoStoredSelection) {
		t.Fatal("unknown-tool must not collapse into ErrNoStoredSelection")
	}
	if cursor.checked != 0 {
		t.Fatal("CheckLogin should not run when lookup fails")
	}
}

func TestFromStore_CheckLoginError(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")
	cursor.loginErr = errors.New("not logged in")

	_, _, err := AgentFromStore(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
	if cursor.checked != 1 {
		t.Fatalf("CheckLogin called %d times, want 1", cursor.checked)
	}
}

// TestFromStore_StoreReadError covers the wrap path when the
// underlying store returns an error: a closed bbolt DB rejects every
// View call, so List surfaces that failure and FromStore wraps it.
func TestFromStore_StoreReadError(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	_, _, err := AgentFromStore(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor})
	if err == nil || !strings.Contains(err.Error(), "resolver: read planner") {
		t.Fatalf("err = %v, want wrapped read error", err)
	}
	if errors.Is(err, ErrNoStoredSelection) {
		t.Fatal("read errors must not collapse into ErrNoStoredSelection")
	}
}

func TestFromStore_HappyPath(t *testing.T) {
	s := openTestStore(t, store.BucketWorker)
	if err := s.Put(store.BucketWorker, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketWorker, "model", "gpt-5"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4", "gpt-5")
	codex := newStubAgent("codex", "o4")

	agent, model, err := AgentFromStore(t.Context(), s, store.BucketWorker, []codingagents.Agent{codex, cursor})
	if err != nil {
		t.Fatalf("FromStore: %v", err)
	}
	if agent != cursor {
		t.Fatalf("agent = %v, want cursor", agent.Name())
	}
	if model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", model)
	}
	if cursor.checked != 1 {
		t.Fatalf("CheckLogin called %d times, want 1", cursor.checked)
	}
	// FromStore must not list models — that's prompt-time work.
	if cursor.listed != 0 {
		t.Fatalf("ListModels called %d times, want 0", cursor.listed)
	}
}

// TestFromStore_DoesNotPersist confirms FromStore never re-Puts the
// values it reads, so the contract "from-settings is read-only" is
// observable from the bucket contents.
func TestFromStore_DoesNotPersist(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	if _, _, err := AgentFromStore(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor}); err != nil {
		t.Fatalf("FromStore: %v", err)
	}
	entries, err := s.List(store.BucketPlanner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	keys := make([]string, len(entries))
	for i, kv := range entries {
		keys[i] = kv.Key
	}
	want := []string{"model", "tool"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("planner keys = %v, want %v (FromStore must not Put)", keys, want)
	}
}

// TestStoredInteractive_NilStore documents that the read-only helper
// returns (false, false) when no store is configured, so callers
// can pipe Options.Store straight in without a nil check.
// TestStoredInteractive_Missing covers the bucket-exists-but-no-key
// branch: ok must be false and v must be the zero value.
// TestStoredInteractive_Empty asserts that an empty stored value is
// treated as "not set" so callers fall back to the cobra default.
// TestStoredInteractive_Unparseable asserts that a non-bool string
// (e.g. corruption) collapses to the not-set sentinel so we never
// surface a parse error to the caller.
// TestStoredInteractive_True round-trips strconv.FormatBool(true).
// TestStoredInteractive_False round-trips strconv.FormatBool(false).
// TestStoredInteractive_GetError covers the `err != nil` branch in
// the helper: a closed bbolt DB rejects every View call so Get
// surfaces the failure and StoredInteractive returns the
// not-set sentinel rather than propagating the error.
// TestResolve_BothExplicit short-circuits the store entirely:
// neither key is read, the agent runs CheckLogin, and the bucket is
// not consulted. A nil store is fine.
func TestResolve_BothExplicit(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4")
	agent, model, err := resolveAgent(t.Context(), nil, store.BucketPlanner, []codingagents.Agent{cursor}, "cursor", "opus")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if agent != cursor {
		t.Fatalf("agent = %v, want cursor", agent.Name())
	}
	if model != "opus" {
		t.Fatalf("model = %q, want opus", model)
	}
	if cursor.checked != 1 {
		t.Fatalf("CheckLogin = %d, want 1", cursor.checked)
	}
	// Resolve must not list models — that's prompt-time work.
	if cursor.listed != 0 {
		t.Fatalf("ListModels called %d times, want 0", cursor.listed)
	}
}

// TestResolve_ToolOnly_FillsModelFromStore covers the partial-flag
// path: --tool alone reads model from the bucket.
func TestResolve_ToolOnly_FillsModelFromStore(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "model", "stored-model"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	agent, model, err := resolveAgent(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor}, "cursor", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if agent != cursor {
		t.Fatalf("agent = %v, want cursor", agent.Name())
	}
	if model != "stored-model" {
		t.Fatalf("model = %q, want stored-model", model)
	}
}

// TestResolve_ModelOnly_FillsToolFromStore is the symmetric case.
func TestResolve_ModelOnly_FillsToolFromStore(t *testing.T) {
	s := openTestStore(t, store.BucketWorker)
	if err := s.Put(store.BucketWorker, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	agent, model, err := resolveAgent(t.Context(), s, store.BucketWorker, []codingagents.Agent{cursor}, "", "opus")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if agent != cursor {
		t.Fatalf("agent = %v, want cursor", agent.Name())
	}
	if model != "opus" {
		t.Fatalf("model = %q, want opus", model)
	}
}

// TestResolve_ToolOnly_EmptyBucket errors with a clear hint about
// the missing half so the user knows to pass --model or run
// `j settings reset`.
func TestResolve_ToolOnly_EmptyBucket(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	cursor := newStubAgent("cursor", "sonnet-4")

	_, _, err := resolveAgent(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor}, "cursor", "")
	if err == nil || !strings.Contains(err.Error(), "given without stored model in planner") {
		t.Fatalf("err = %v, want missing-model error", err)
	}
	if cursor.checked != 0 {
		t.Fatal("CheckLogin must not run when the missing half is unavailable")
	}
}

// TestResolve_ModelOnly_EmptyBucket is the symmetric case.
func TestResolve_ModelOnly_EmptyBucket(t *testing.T) {
	s := openTestStore(t, store.BucketVerifier)
	cursor := newStubAgent("cursor", "sonnet-4")

	_, _, err := resolveAgent(t.Context(), s, store.BucketVerifier, []codingagents.Agent{cursor}, "", "opus")
	if err == nil || !strings.Contains(err.Error(), "given without stored tool in verifier") {
		t.Fatalf("err = %v, want missing-tool error", err)
	}
	if cursor.checked != 0 {
		t.Fatal("CheckLogin must not run when the missing half is unavailable")
	}
}

// TestResolve_BothEmpty signals to the caller (selectPlanner /
// selectWorker / selectVerifier) that no override was requested so it
// can fall through to FromStore / Pick.
func TestResolve_BothEmpty(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4")
	_, _, err := resolveAgent(t.Context(), nil, store.BucketPlanner, []codingagents.Agent{cursor}, "", "")
	if !errors.Is(err, ErrNoStoredSelection) {
		t.Fatalf("err = %v, want ErrNoStoredSelection", err)
	}
}

// TestResolve_UnknownTool errors on an off-list explicit tool name
// before CheckLogin runs.
func TestResolve_UnknownTool(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4")
	_, _, err := resolveAgent(t.Context(), nil, store.BucketPlanner, []codingagents.Agent{cursor}, "ghost", "opus")
	if err == nil || !strings.Contains(err.Error(), `unknown tool "ghost"`) {
		t.Fatalf("err = %v, want unknown-tool error", err)
	}
	if cursor.checked != 0 {
		t.Fatal("CheckLogin must not run when lookup fails")
	}
}

// TestResolve_CheckLoginError propagates the agent's auth failure.
func TestResolve_CheckLoginError(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4")
	cursor.loginErr = errors.New("not logged in")

	_, _, err := resolveAgent(t.Context(), nil, store.BucketPlanner, []codingagents.Agent{cursor}, "cursor", "opus")
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v, want login error", err)
	}
	if cursor.checked != 1 {
		t.Fatalf("CheckLogin called %d times, want 1", cursor.checked)
	}
}

// TestResolve_StoreReadError surfaces non-sentinel read failures
// rather than silently treating them as a missing key.
func TestResolve_StoreReadError(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	_, _, err := resolveAgent(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor}, "cursor", "")
	if err == nil || !strings.Contains(err.Error(), "resolver: read planner") {
		t.Fatalf("err = %v, want wrapped read error", err)
	}
}

// TestResolve_DoesNotPersist confirms Resolve never writes the
// values it reads, so the contract "explicit overrides are one-off"
// is observable from the bucket contents.
func TestResolve_DoesNotPersist(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")

	if _, _, err := resolveAgent(t.Context(), s, store.BucketPlanner, []codingagents.Agent{cursor}, "", "opus"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	entries, err := s.List(store.BucketPlanner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Key != "tool" {
		t.Fatalf("planner entries = %v, want only the original tool key", entries)
	}
}

// TestAgent_ExplicitHits pins the explicit-flag fast path: both flags
// set → resolveAgent runs, no store read, no prompt.
func TestAgent_ExplicitHits(t *testing.T) {
	cursor := newStubAgent("cursor", "sonnet-4")
	ui := &scriptedUI{}
	agent, model, err := Agent(t.Context(), AgentOptions{
		Bucket:        store.BucketPlanner,
		Agents:        []codingagents.Agent{cursor},
		ExplicitTool:  "cursor",
		ExplicitModel: "sonnet-4",
		UI:            ui,
		Stderr:        io.Discard,
	})
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}
	if agent != cursor || model != "sonnet-4" {
		t.Fatalf("agent=%v model=%q", agent.Name(), model)
	}
	if ui.toolCalls != 0 || ui.modelCalls != 0 {
		t.Fatalf("UI prompts should not fire on explicit path")
	}
}

// TestAgent_StoredHits pins the stored-fast-path: bucket has tool +
// model, no explicit flags → AgentFromStore returns, no prompt.
func TestAgent_StoredHits(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatal(err)
	}
	cursor := newStubAgent("cursor", "sonnet-4")
	ui := &scriptedUI{}
	agent, model, err := Agent(t.Context(), AgentOptions{
		Bucket: store.BucketPlanner,
		Agents: []codingagents.Agent{cursor},
		UI:     ui,
		Store:  s,
	})
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}
	if agent != cursor || model != "sonnet-4" {
		t.Fatalf("agent=%v model=%q", agent.Name(), model)
	}
	if ui.toolCalls != 0 {
		t.Fatalf("SelectTool should not fire when stored")
	}
}

// TestAgent_PromptsAndPersists pins the empty-bucket cold path: PickAgent
// fires + persistAgent writes tool/model/interactive.
func TestAgent_PromptsAndPersists(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	cursor := newStubAgent("cursor", "sonnet-4")
	ui := &scriptedUI{tool: "cursor", model: "sonnet-4"}
	var stderr bytes.Buffer
	agent, model, err := Agent(t.Context(), AgentOptions{
		Bucket:      store.BucketPlanner,
		Agents:      []codingagents.Agent{cursor},
		UI:          ui,
		Store:       s,
		Stderr:      &stderr,
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}
	if agent != cursor || model != "sonnet-4" {
		t.Fatalf("agent=%v model=%q", agent.Name(), model)
	}
	if ui.toolCalls != 1 || ui.modelCalls != 1 {
		t.Fatalf("prompts: tool=%d model=%d, want 1/1", ui.toolCalls, ui.modelCalls)
	}
	if !strings.Contains(stderr.String(), "Choose your favourite") {
		t.Fatalf("stderr = %q, want prompt label", stderr.String())
	}
	for _, want := range []struct{ k, v string }{{"tool", "cursor"}, {"model", "sonnet-4"}, {"interactive", "false"}} {
		v, ok, err := s.Get(store.BucketPlanner, want.k)
		if err != nil {
			t.Fatalf("Get %s: %v", want.k, err)
		}
		if !ok || v != want.v {
			t.Fatalf("planner.%s = %q (ok=%v), want %q", want.k, v, ok, want.v)
		}
	}
}

// TestAgent_PromptError surfaces a UI error from PickAgent without
// persisting.
func TestAgent_PromptError(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	cursor := newStubAgent("cursor", "sonnet-4")
	ui := &scriptedUI{toolErr: errors.New("ui boom")}
	_, _, err := Agent(t.Context(), AgentOptions{
		Bucket: store.BucketPlanner,
		Agents: []codingagents.Agent{cursor},
		UI:     ui,
		Store:  s,
	})
	if err == nil || !strings.Contains(err.Error(), "ui boom") {
		t.Fatalf("err = %v, want 'ui boom'", err)
	}
	entries, err := s.List(store.BucketPlanner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("planner bucket should stay empty on prompt error, got %v", entries)
	}
}

// TestAgent_ExplicitMissingHalfPropagates pins that resolveExplicit's
// "given without stored" error reaches the caller verbatim and does
// NOT fall through to AgentFromStore.
func TestAgent_ExplicitMissingHalfPropagates(t *testing.T) {
	s := openTestStore(t, store.BucketPlanner)
	cursor := newStubAgent("cursor", "sonnet-4")
	ui := &scriptedUI{}
	_, _, err := Agent(t.Context(), AgentOptions{
		Bucket:       store.BucketPlanner,
		Agents:       []codingagents.Agent{cursor},
		ExplicitTool: "cursor",
		UI:           ui,
		Store:        s,
	})
	if err == nil || !strings.Contains(err.Error(), "given without stored model") {
		t.Fatalf("err = %v, want 'given without stored model'", err)
	}
	if ui.toolCalls != 0 {
		t.Fatalf("UI must not fire on explicit-missing-half error")
	}
}

func TestAgent_LazyStorePaths(t *testing.T) {
	setupResolverProject(t)
	cursor := newStubAgent("cursor", "sonnet-4")

	agent, model, err := Agent(t.Context(), AgentOptions{
		Bucket:        store.BucketPlanner,
		Agents:        []codingagents.Agent{cursor},
		ExplicitTool:  "cursor",
		ExplicitModel: "sonnet-4",
		Stderr:        io.Discard,
	})
	if err != nil || agent != cursor || model != "sonnet-4" {
		t.Fatalf("explicit lazy = %v %q %v", agent, model, err)
	}

	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(store.BucketPlanner); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketPlanner, "model", "sonnet-4"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	agent, model, err = Agent(t.Context(), AgentOptions{
		Bucket: store.BucketPlanner,
		Agents: []codingagents.Agent{cursor},
		Stderr: io.Discard,
	})
	if err != nil || agent != cursor || model != "sonnet-4" {
		t.Fatalf("stored lazy = %v %q %v", agent, model, err)
	}
}

func TestAgent_LazyPromptPersists(t *testing.T) {
	setupResolverProject(t)
	cursor := newStubAgent("cursor", "sonnet-4")
	ui := &scriptedUI{tool: "cursor", model: "sonnet-4"}
	agent, model, err := Agent(t.Context(), AgentOptions{
		Bucket:      store.BucketPlanner,
		Agents:      []codingagents.Agent{cursor},
		UI:          ui,
		Stderr:      io.Discard,
		Interactive: true,
	})
	if err != nil || agent != cursor || model != "sonnet-4" {
		t.Fatalf("prompt lazy = %v %q %v", agent, model, err)
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	got, ok, err := s.Get(store.BucketPlanner, "interactive")
	if err != nil || !ok || got != "true" {
		t.Fatalf("interactive = %q %v %v", got, ok, err)
	}
}

func TestResolveToolModel_BothExplicit(t *testing.T) {
	tool, model := ResolveToolModel("cursor", "sonnet", store.BucketWorker, io.Discard)
	if tool != "cursor" || model != "sonnet" {
		t.Fatalf("got (%q, %q), want (cursor, sonnet)", tool, model)
	}
}

func TestResolveToolModel_FillsFromStore(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	ss, ok := store.OpenSettings(io.Discard)
	if !ok {
		t.Fatal("OpenSettings failed")
	}
	if err := ss.Put(store.BucketWorker, "tool", "stored-tool"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := ss.Put(store.BucketWorker, "model", "stored-model"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	_ = ss.Close()
	tool, model := ResolveToolModel("", "explicit-model", store.BucketWorker, io.Discard)
	if tool != "stored-tool" {
		t.Fatalf("tool = %q, want stored-tool", tool)
	}
	if model != "explicit-model" {
		t.Fatalf("model = %q, want explicit-model (already set)", model)
	}
}

func TestResolveToolModel_StoreOpenFails(t *testing.T) {
	// In a non-.j directory, store.OpenSettings returns ("", false).
	t.Chdir(t.TempDir())
	tool, model := ResolveToolModel("", "", store.BucketWorker, io.Discard)
	if tool != "" || model != "" {
		t.Fatalf("got (%q, %q), want empty (store not available)", tool, model)
	}
}
