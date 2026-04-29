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

// TestHuhUI_PickPlanTask_EmptyTasks pins the early-return
// validation: PickPlanTask refuses an empty slice instead of
// trying to render a huh widget without options.
func TestHuhUI_PickPlanTask_EmptyTasks(t *testing.T) {
	u := newHuhUI(strings.NewReader(""), io.Discard)
	_, err := u.PickPlanTask(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no plan sessions") {
		t.Fatalf("err = %v, want 'no plan sessions' error", err)
	}
}
