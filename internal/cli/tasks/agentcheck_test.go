package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// scriptedSelector is an in-package fake that satisfies AgentSelector
// (and, since the surface is identical, agentpick.Selector). The zero
// value picks the first option for both prompts so most tests need
// only set the fields they assert on.
type scriptedSelector struct {
	tool     string
	model    string
	toolErr  error
	modelErr error

	toolCalls  int
	modelCalls int
	lastTools  []string
	lastModels []string
}

func (s *scriptedSelector) SelectTool(_ context.Context, options []string) (string, error) {
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

func (s *scriptedSelector) SelectModel(_ context.Context, options []string) (string, error) {
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

// scriptedAgent stands in for a real codingagents.Agent in tests. Plan
// and Work / Verify are unused by the agentcheck tests so they return
// errors to make accidental invocation loud.
type scriptedAgent struct {
	name      string
	models    []string
	modelsErr error
	loginErr  error
}

func newScriptedAgent() *scriptedAgent {
	return &scriptedAgent{name: "cursor", models: []string{"sonnet-4", "gpt-5"}}
}

func (a *scriptedAgent) Name() string                                   { return a.name }
func (a *scriptedAgent) ListModels(context.Context) ([]string, error)   { return a.models, a.modelsErr }
func (a *scriptedAgent) CheckLogin(context.Context) error               { return a.loginErr }
func (a *scriptedAgent) NewResumeID(context.Context) (string, error)    { return "rid", nil }
func (a *scriptedAgent) Plan(context.Context, codingagents.PlanRequest) (int, error) {
	return 0, errors.New("scriptedAgent.Plan should not be called")
}
func (a *scriptedAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	return 0, errors.New("scriptedAgent.Work should not be called")
}
func (a *scriptedAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("scriptedAgent.Verify should not be called")
}

// seedAgentBucket pre-populates a single bucket with a tool/model
// pair so EnsureAgentSelections sees it as already-configured.
func seedAgentBucket(t *testing.T, bucket, tool, model string) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.EnsureBucket(bucket); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	if err := s.Put(bucket, "tool", tool); err != nil {
		t.Fatalf("Put tool: %v", err)
	}
	if err := s.Put(bucket, "model", model); err != nil {
		t.Fatalf("Put model: %v", err)
	}
}

// readAgentBucket returns the (tool, model, interactive) recorded in
// bucket. Empty strings for missing entries.
func readAgentBucket(t *testing.T, bucket string) (string, string, string) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	tool, _, _ := s.Get(bucket, "tool")
	model, _, _ := s.Get(bucket, "model")
	interactive, _, _ := s.Get(bucket, "interactive")
	return tool, model, interactive
}

// TestEnsureAgentSelections_AllBucketsPopulated pins the no-prompt
// happy path: every bucket already has a tool/model pair, so the
// selector is never invoked and no bucket is mutated.
func TestEnsureAgentSelections_AllBucketsPopulated(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	sel := &scriptedSelector{}
	err := EnsureAgentSelections(context.Background(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     sel,
	})
	if err != nil {
		t.Fatalf("EnsureAgentSelections: %v", err)
	}
	if sel.toolCalls != 0 || sel.modelCalls != 0 {
		t.Fatalf("selector called: tools=%d models=%d, want 0/0", sel.toolCalls, sel.modelCalls)
	}
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		tool, model, interactive := readAgentBucket(t, bucket)
		if tool != "cursor" || model != "sonnet-4" {
			t.Fatalf("bucket %q changed: tool=%q model=%q", bucket, tool, model)
		}
		if interactive != "" {
			t.Fatalf("bucket %q gained interactive=%q (should not write existing buckets)", bucket, interactive)
		}
	}
}

// TestEnsureAgentSelections_AllBucketsEmpty pins the prompt-three-
// times path: every bucket is empty, so the selector is invoked
// three times (once per bucket) and each bucket is persisted with
// tool/model/interactive=true.
func TestEnsureAgentSelections_AllBucketsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	sel := &scriptedSelector{tool: "cursor", model: "sonnet-4"}
	err := EnsureAgentSelections(context.Background(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     sel,
	})
	if err != nil {
		t.Fatalf("EnsureAgentSelections: %v", err)
	}
	if sel.toolCalls != 3 || sel.modelCalls != 3 {
		t.Fatalf("selector calls: tools=%d models=%d, want 3/3", sel.toolCalls, sel.modelCalls)
	}
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		tool, model, interactive := readAgentBucket(t, bucket)
		if tool != "cursor" || model != "sonnet-4" {
			t.Fatalf("bucket %q = (%q, %q), want (cursor, sonnet-4)", bucket, tool, model)
		}
		if interactive != "true" {
			t.Fatalf("bucket %q interactive = %q, want \"true\"", bucket, interactive)
		}
	}
}

// TestEnsureAgentSelections_PartialBuckets pins the mixed case: the
// planner bucket is populated but the worker and verifier are empty,
// so the selector is invoked twice (once per missing bucket).
func TestEnsureAgentSelections_PartialBuckets(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedAgentBucket(t, store.BucketPlanner, "cursor", "gpt-5")
	sel := &scriptedSelector{tool: "cursor", model: "sonnet-4"}
	err := EnsureAgentSelections(context.Background(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     sel,
	})
	if err != nil {
		t.Fatalf("EnsureAgentSelections: %v", err)
	}
	if sel.toolCalls != 2 {
		t.Fatalf("toolCalls = %d, want 2", sel.toolCalls)
	}
	tool, model, _ := readAgentBucket(t, store.BucketPlanner)
	if tool != "cursor" || model != "gpt-5" {
		t.Fatalf("planner bucket overwritten: (%q, %q)", tool, model)
	}
	for _, bucket := range []string{store.BucketWorker, store.BucketVerifier} {
		tool, model, interactive := readAgentBucket(t, bucket)
		if tool != "cursor" || model != "sonnet-4" || interactive != "true" {
			t.Fatalf("bucket %q = (%q, %q, %q), want (cursor, sonnet-4, true)", bucket, tool, model, interactive)
		}
	}
}

// TestEnsureAgentSelections_NoAgents pins the error branch when the
// caller forgets to wire any agents.
func TestEnsureAgentSelections_NoAgents(t *testing.T) {
	err := EnsureAgentSelections(context.Background(), AgentCheckOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

// TestEnsureAgentSelections_SelectorAborts pins the user-cancel
// branch: huh.ErrUserAborted from the selector propagates verbatim
// so the caller's deferred guard can convert it to a nil exit.
func TestEnsureAgentSelections_SelectorAborts(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	sel := &scriptedSelector{toolErr: huh.ErrUserAborted}
	err := EnsureAgentSelections(context.Background(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     sel,
	})
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("err = %v, want huh.ErrUserAborted", err)
	}
}

// TestEnsureAgentSelections_ListModelsFails covers the FromStore
// error path: a populated bucket points at a real agent whose
// ListModels fails downstream of CheckLogin — but here it triggers
// CheckLogin via FromStore. We exercise that path by seeding a
// bucket with a tool name that does not match any agent.
func TestEnsureAgentSelections_FromStoreUnknownTool(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedAgentBucket(t, store.BucketPlanner, "ghost", "sonnet-4")
	err := EnsureAgentSelections(context.Background(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     &scriptedSelector{},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown tool "ghost"`) {
		t.Fatalf("err = %v", err)
	}
}

// TestEnsureAgentSelections_AppliesDefaults exercises the
// withDefaults branch: passing only Agents must not panic and must
// fall back to the huh selector. We populate every bucket so the
// selector is never invoked (so the huh widget never reads stdin).
func TestEnsureAgentSelections_AppliesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	if err := EnsureAgentSelections(context.Background(), AgentCheckOptions{
		Agents: []codingagents.Agent{newScriptedAgent()},
	}); err != nil {
		t.Fatalf("EnsureAgentSelections: %v", err)
	}
}

// TestEnsureAgentSelections_StoreOpenFailure simulates a corrupt
// settings layout (a regular file at .j/settings) so store.OpenSettings
// fails. EnsureAgentSelections treats the open failure as
// ErrNoStoredSelection (so the flow falls into Pick) and the persist
// step also short-circuits silently — net effect: every bucket prompts,
// nothing is written, no error is returned.
func TestEnsureAgentSelections_StoreOpenFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	// Replace the freshly-created settings file with a directory so
	// bolt.Open fails on every subsequent EnsureAgentSelections open.
	if err := removeAndMkdir(path); err != nil {
		t.Fatalf("replace settings with dir: %v", err)
	}
	sel := &scriptedSelector{tool: "cursor", model: "sonnet-4"}
	var stderr bytes.Buffer
	err = EnsureAgentSelections(context.Background(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     sel,
	})
	if err != nil {
		t.Fatalf("EnsureAgentSelections: %v", err)
	}
	if sel.toolCalls != 3 {
		t.Fatalf("toolCalls = %d, want 3 (every bucket should prompt when settings is unreadable)", sel.toolCalls)
	}
	if !strings.Contains(stderr.String(), "warning: settings db") {
		t.Fatalf("stderr should warn about settings db: %q", stderr.String())
	}
}

// removeAndMkdir is a tiny helper that swaps a regular file for an
// empty directory at path, used by TestEnsureAgentSelections_StoreOpenFailure
// to break store.OpenSettings.
func removeAndMkdir(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return os.MkdirAll(path, 0o755)
}
