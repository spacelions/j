package picker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/linear"
)

// dumbTerm sets TERM=dumb for the duration of the test so huh uses
// accessible mode (plain-text prompts) instead of bubbletea's TUI
// loop. This lets tests supply input via strings.NewReader without
// a real TTY. Each test that drives a huh form must call this.
func dumbTerm(t *testing.T) {
	t.Helper()
	t.Setenv("TERM", "dumb")
}

func TestNew(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	if p == nil {
		t.Fatal("New returned nil")
	}
}

func TestChoose_EmptyOptions(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.choose(t.Context(), "Select model", nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v, want 'no options'", err)
	}
}

func TestChoose_ContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.choose(ctx, "Select model", []string{"a"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestChoose_HappyPath exercises the select picker in accessible mode.
// TERM=dumb activates huh's accessible mode which reads one line per
// choice; a blank line selects the first option.
func TestChoose_HappyPath(t *testing.T) {
	dumbTerm(t)
	p := New(strings.NewReader("\n"), io.Discard)
	got, err := p.choose(t.Context(), "Pick", []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "alpha" {
		t.Fatalf("got = %q, want alpha", got)
	}
}

func TestSelectTool_EmptyOptions(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.SelectTool(t.Context(), nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v", err)
	}
}

// TestSelectTool_HappyPath drives SelectTool via accessible mode.
func TestSelectTool_HappyPath(t *testing.T) {
	dumbTerm(t)
	p := New(strings.NewReader("\n"), io.Discard)
	got, err := p.SelectTool(t.Context(), []string{"cursor", "claude"})
	if err != nil {
		t.Fatalf("SelectTool: %v", err)
	}
	if got != "cursor" {
		t.Fatalf("got = %q, want cursor", got)
	}
}

func TestSelectModel_EmptyOptions(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.SelectModel(t.Context(), nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v", err)
	}
}

// TestSelectModel_HappyPath drives SelectModel via accessible mode.
func TestSelectModel_HappyPath(t *testing.T) {
	dumbTerm(t)
	p := New(strings.NewReader("\n"), io.Discard)
	got, err := p.SelectModel(t.Context(), []string{"sonnet-4", "opus-4"})
	if err != nil {
		t.Fatalf("SelectModel: %v", err)
	}
	if got != "sonnet-4" {
		t.Fatalf("got = %q, want sonnet-4", got)
	}
}

func TestSelectSource_EmptyAllowed(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.SelectSource(t.Context(), nil)
	if err == nil || !strings.Contains(err.Error(), "no sources") {
		t.Fatalf("err = %v, want 'no sources'", err)
	}
}

// TestSelectSource_HappyPath drives SelectSource via accessible mode.
func TestSelectSource_HappyPath(t *testing.T) {
	dumbTerm(t)
	p := New(strings.NewReader("\n"), io.Discard)
	got, err := p.SelectSource(
		t.Context(),
		[]Source{SourceMarkdown, SourceLinear},
	)
	if err != nil {
		t.Fatalf("SelectSource: %v", err)
	}
	if got != SourceMarkdown {
		t.Fatalf("got = %q, want markdown", got)
	}
}

func TestPickMarkdownInCwd_NoFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.PickMarkdownInCwd(t.Context())
	if err == nil || !strings.Contains(err.Error(), "no markdown files") {
		t.Fatalf("err = %v, want 'no markdown files'", err)
	}
}

// TestPickMarkdownInCwd_HappyPath drives PickMarkdownInCwd in
// accessible mode.
func TestPickMarkdownInCwd_HappyPath(t *testing.T) {
	dumbTerm(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "feature.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p := New(strings.NewReader("\n"), io.Discard)
	got, err := p.PickMarkdownInCwd(t.Context())
	if err != nil {
		t.Fatalf("PickMarkdownInCwd: %v", err)
	}
	if !strings.HasSuffix(got, "feature.md") {
		t.Fatalf("got = %q, want feature.md", got)
	}
}

// TestConfirmStatusOverride_Yes drives ConfirmStatusOverride via
// accessible mode: "y\n" selects yes.
func TestConfirmStatusOverride_Yes(t *testing.T) {
	dumbTerm(t)
	p := New(strings.NewReader("y\n"), io.Discard)
	got, err := p.ConfirmStatusOverride(t.Context(), "plan", "T1", "working")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got {
		t.Fatal("expected true for y input")
	}
}

// TestConfirmStatusOverride_No drives the no branch.
func TestConfirmStatusOverride_No(t *testing.T) {
	dumbTerm(t)
	p := New(strings.NewReader("n\n"), io.Discard)
	got, err := p.ConfirmStatusOverride(t.Context(), "plan", "T1", "working")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got {
		t.Fatal("expected false for n input")
	}
}

// TestConfirmStatusOverride_CancelledCtx exercises the
// context-cancelled error path.
func TestConfirmStatusOverride_CancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := New(strings.NewReader(""), io.Discard)
	_, err := p.ConfirmStatusOverride(ctx, "plan", "T1", "working")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestPromptLinearAPIKey_ContextCancelled exercises the error-return
// path in PromptLinearAPIKey: a cancelled context makes run() return
// a context error before the form renders, so the function surfaces
// (empty, false, err).
func TestPromptLinearAPIKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := New(strings.NewReader(""), io.Discard)
	_, ok, err := p.PromptLinearAPIKey(ctx, "https://example.com")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if ok {
		t.Fatal("ok = true, want false on error")
	}
}

// TestPickLinearIssue_HappyPath drives the issue picker in accessible
// mode.
func TestPickLinearIssue_HappyPath(t *testing.T) {
	dumbTerm(t)
	p := New(strings.NewReader("\n"), io.Discard)
	issues := []linear.Issue{
		{Identifier: "ENG-1", Title: "first issue", State: "In Progress"},
		{Identifier: "ENG-2", Title: "second", State: "Todo"},
	}
	iss, ok, err := p.PickLinearIssue(t.Context(), issues)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatal("ok = false")
	}
	if iss.Identifier != "ENG-1" {
		t.Fatalf("identifier = %q, want ENG-1", iss.Identifier)
	}
}

// TestPickLinearProject_HappyPath drives the project picker in
// accessible mode.
func TestPickLinearProject_HappyPath(t *testing.T) {
	dumbTerm(t)
	p := New(strings.NewReader("\n"), io.Discard)
	projects := []linear.Project{
		{ID: "p1", Name: "Alpha"},
		{ID: "p2", Name: "Beta"},
	}
	prj, ok, err := p.PickLinearProject(t.Context(), projects)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatal("ok = false")
	}
	if prj.ID != "p1" {
		t.Fatalf("id = %q, want p1", prj.ID)
	}
}

// TestPickTask_HappyPath drives PickTask in accessible mode.
func TestPickTask_HappyPath(t *testing.T) {
	dumbTerm(t)
	p := New(strings.NewReader("\n"), io.Discard)
	taskRows := []tasks.Task{
		{ID: "T1", Status: tasks.StatusPlanning, Summary: "first task"},
		{ID: "T2", Status: tasks.StatusPlanDone, Summary: "second task"},
	}
	id, ok, err := p.PickTask(t.Context(), "Pick a task", taskRows)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatal("ok = false")
	}
	if id != "T1" {
		t.Fatalf("id = %q, want T1", id)
	}
}

// TestPickTask_ContextCancelled drives the cancelled-context path.
func TestPickTask_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := New(strings.NewReader(""), io.Discard)
	taskRows := []tasks.Task{{ID: "T1", Status: tasks.StatusPlanning}}
	_, _, err := p.PickTask(ctx, "Pick", taskRows)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
