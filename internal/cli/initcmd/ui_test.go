package initcmd

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestAccept covers every branch of the free-form-input mapping
// helper used by alternative UIs and tests that need to translate
// user-typed answers ("yes", "y", " Y ") into the same boolean as
// the canonical huh.NewConfirm widget.
func TestAccept(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"y", true},
		{"Y", true},
		{"yes", true},
		{"YES", true},
		{"  yes ", true},
		{"n", false},
		{"no", false},
		{"nope", false},
		{"  no", false},
		{"\tnope\n", false},
	}
	for _, c := range cases {
		if got := Accept(c.in); got != c.want {
			t.Errorf("Accept(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestNewHuhUI_NotNil pins the constructor: it returns a non-nil
// huh-backed UI implementation. Driving the form requires a TTY so
// the body of ConfirmReset is exercised through the UI interface in
// the orchestration tests in cmd_test.go.
func TestNewHuhUI_NotNil(t *testing.T) {
	if u := newHuhUI(&bytes.Buffer{}, &bytes.Buffer{}); u == nil {
		t.Fatal("newHuhUI returned nil")
	}
}

// TestConfirmReset_Yes exercises ConfirmReset in huh accessible mode
// (TERM=dumb). Input "y\n" means yes.
func TestConfirmReset_Yes(t *testing.T) {
	t.Setenv("TERM", "dumb")
	u := newHuhUI(strings.NewReader("y\n"), io.Discard)
	got, err := u.ConfirmReset(t.Context())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got {
		t.Fatal("expected true for y input")
	}
}

// TestConfirmReset_No exercises ConfirmReset in accessible mode.
func TestConfirmReset_No(t *testing.T) {
	t.Setenv("TERM", "dumb")
	u := newHuhUI(strings.NewReader("n\n"), io.Discard)
	got, err := u.ConfirmReset(t.Context())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got {
		t.Fatal("expected false for n input")
	}
}
