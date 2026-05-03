package plan

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestNewHuhUI(t *testing.T) {
	if u := newHuhUI(strings.NewReader(""), io.Discard); u == nil {
		t.Fatal("newHuhUI returned nil")
	}
}

func TestHuhUI_Choose_EmptyOptions(t *testing.T) {
	u := newHuhUI(strings.NewReader(""), io.Discard)
	_, err := u.choose(context.Background(), "Select model", nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v, want 'no options' error", err)
	}
}

func TestHuhUI_SelectTool_EmptyOptions(t *testing.T) {
	u := newHuhUI(strings.NewReader(""), io.Discard)
	_, err := u.SelectTool(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v", err)
	}
}

func TestHuhUI_SelectModel_EmptyOptions(t *testing.T) {
	u := newHuhUI(strings.NewReader(""), io.Discard)
	_, err := u.SelectModel(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v", err)
	}
}

// TestHuhUI_PickFromFile_EmptyOptions pins that an empty option
// slice surfaces the shared `choose` validation error rather than
// trying to render a huh widget without options. The orchestrator
// short-circuits an empty scan upstream so this branch is defensive,
// but exercising it keeps the contract uniform with the other
// pickers.
func TestHuhUI_PickFromFile_EmptyOptions(t *testing.T) {
	u := newHuhUI(strings.NewReader(""), io.Discard)
	_, err := u.PickFromFile(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no options") {
		t.Fatalf("err = %v, want 'no options' error", err)
	}
}

// PickPlanTask / PickReplanTask are one-line delegates to
// internal/cli/taskpick.Pick; the empty-input contract is pinned
// once in taskpick_test.go (TestPick_EmptyTasks) and not
// re-asserted here.
