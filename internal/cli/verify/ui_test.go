package verify

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

// PickWorkDoneTask / PickVerifyTask are one-line delegates to
// internal/cli/taskpick.Pick; the empty-input contract is pinned
// once in taskpick_test.go (TestPick_EmptyTasks) and not
// re-asserted here.

// TestErrEmptyFromFile_Message pins the user-facing string of the
// AskFromFile empty-input branch.
func TestErrEmptyFromFile_Message(t *testing.T) {
	if got := errEmptyFromFile.Error(); got != "J: no markdown provided" {
		t.Fatalf("errEmptyFromFile.Error() = %q, want %q", got, "J: no markdown provided")
	}
}
