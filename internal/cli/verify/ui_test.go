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

// TestHuhUI_PickWorkDoneTask_EmptyTasks pins the empty-slice early
// return for the regular `j verify` picker.
func TestHuhUI_PickWorkDoneTask_EmptyTasks(t *testing.T) {
	u := newHuhUI(strings.NewReader(""), io.Discard)
	_, err := u.PickWorkDoneTask(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no tasks") {
		t.Fatalf("err = %v", err)
	}
}

// TestHuhUI_PickVerifyTask_EmptyTasks pins the empty-slice early
// return for the resume picker variant.
func TestHuhUI_PickVerifyTask_EmptyTasks(t *testing.T) {
	u := newHuhUI(strings.NewReader(""), io.Discard)
	_, err := u.PickVerifyTask(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no tasks") {
		t.Fatalf("err = %v", err)
	}
}

// TestErrEmptyFromFile_Message pins the user-facing string of the
// AskFromFile empty-input branch.
func TestErrEmptyFromFile_Message(t *testing.T) {
	if got := errEmptyFromFile.Error(); got != "J: no markdown provided" {
		t.Fatalf("errEmptyFromFile.Error() = %q, want %q", got, "J: no markdown provided")
	}
}
