package initcmd

import (
	"bytes"
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
