package preflight

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
)

func mustInit(t *testing.T) {
	t.Helper()
	testutil.Init(t)
}

func TestEnsureAgentSelections_AllBucketsPopulated(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	sel := &testutil.SelectorFake{}
	err := EnsureAgentSelections(t.Context(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{testutil.NewScriptedAgent()},
		UI:     sel,
	})
	if err != nil {
		t.Fatalf("EnsureAgentSelections: %v", err)
	}
	if sel.ToolCalls != 0 || sel.ModelCalls != 0 {
		t.Fatalf("selector called: tools=%d models=%d, want 0/0", sel.ToolCalls, sel.ModelCalls)
	}
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		tool, model, interactive := testutil.ReadAgentBucket(t, bucket)
		if tool != "cursor" || model != "sonnet-4" {
			t.Fatalf("bucket %q changed: tool=%q model=%q", bucket, tool, model)
		}
		if interactive != "" {
			t.Fatalf("bucket %q gained interactive=%q (should not write existing buckets)", bucket, interactive)
		}
	}
}

func TestEnsureAgentSelections_AllBucketsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	sel := &testutil.SelectorFake{Tool: "cursor", Model: "sonnet-4"}
	err := EnsureAgentSelections(t.Context(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{testutil.NewScriptedAgent()},
		UI:     sel,
	})
	if err != nil {
		t.Fatalf("EnsureAgentSelections: %v", err)
	}
	if sel.ToolCalls != 3 || sel.ModelCalls != 3 {
		t.Fatalf("selector calls: tools=%d models=%d, want 3/3", sel.ToolCalls, sel.ModelCalls)
	}
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		tool, model, interactive := testutil.ReadAgentBucket(t, bucket)
		if tool != "cursor" || model != "sonnet-4" {
			t.Fatalf("bucket %q = (%q, %q), want (cursor, sonnet-4)", bucket, tool, model)
		}
		if interactive != "" {
			t.Fatalf("bucket %q gained interactive=%q", bucket, interactive)
		}
	}
}

func TestEnsureAgentSelections_PartialBuckets(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	testutil.SeedAgentBucketToolModel(t, store.BucketPlanner, "cursor", "gpt-5")
	sel := &testutil.SelectorFake{Tool: "cursor", Model: "sonnet-4"}
	err := EnsureAgentSelections(t.Context(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{testutil.NewScriptedAgent()},
		UI:     sel,
	})
	if err != nil {
		t.Fatalf("EnsureAgentSelections: %v", err)
	}
	if sel.ToolCalls != 2 {
		t.Fatalf("toolCalls = %d, want 2", sel.ToolCalls)
	}
	tool, model, _ := testutil.ReadAgentBucket(t, store.BucketPlanner)
	if tool != "cursor" || model != "gpt-5" {
		t.Fatalf("planner bucket overwritten: (%q, %q)", tool, model)
	}
	for _, bucket := range []string{store.BucketWorker, store.BucketVerifier} {
		tool, model, interactive := testutil.ReadAgentBucket(t, bucket)
		if tool != "cursor" || model != "sonnet-4" || interactive != "" {
			t.Fatalf(
				"bucket %q = (%q, %q, %q), want (cursor, sonnet-4, '')",
				bucket, tool, model, interactive,
			)
		}
	}
}

func TestEnsureAgentSelections_NoAgents(t *testing.T) {
	err := EnsureAgentSelections(t.Context(), AgentCheckOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "preflight: no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureAgentSelections_SelectorAborts(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	sel := &testutil.SelectorFake{ToolErr: huh.ErrUserAborted}
	err := EnsureAgentSelections(t.Context(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{testutil.NewScriptedAgent()},
		UI:     sel,
	})
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("err = %v, want huh.ErrUserAborted", err)
	}
}

func TestEnsureAgentSelections_FromStoreUnknownTool(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	testutil.SeedAgentBucketToolModel(t, store.BucketPlanner, "ghost", "sonnet-4")
	err := EnsureAgentSelections(t.Context(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{testutil.NewScriptedAgent()},
		UI:     &testutil.SelectorFake{},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown tool "ghost"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureAgentSelections_AppliesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
	if err := EnsureAgentSelections(t.Context(), AgentCheckOptions{
		Agents: []codingagents.Agent{testutil.NewScriptedAgent()},
	}); err != nil {
		t.Fatalf("EnsureAgentSelections: %v", err)
	}
}

func TestEnsureAgentSelections_StoreOpenFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := removeAndMkdir(path); err != nil {
		t.Fatalf("replace settings with dir: %v", err)
	}
	sel := &testutil.SelectorFake{Tool: "cursor", Model: "sonnet-4"}
	var stderr bytes.Buffer
	err = EnsureAgentSelections(t.Context(), AgentCheckOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{testutil.NewScriptedAgent()},
		UI:     sel,
	})
	if err != nil {
		t.Fatalf("EnsureAgentSelections: %v", err)
	}
	if sel.ToolCalls != 3 {
		t.Fatalf("toolCalls = %d, want 3 (every bucket should prompt when settings is unreadable)", sel.ToolCalls)
	}
	if !strings.Contains(stderr.String(), "settings db") {
		t.Fatalf("stderr should warn about settings db: %q", stderr.String())
	}
}

func removeAndMkdir(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return os.MkdirAll(path, 0o755)
}
