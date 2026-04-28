package web

import (
	"strings"
	"testing"
)

func TestNew_Smoke(t *testing.T) {
	cmd := New()
	if cmd == nil {
		t.Fatal("New returned nil")
	}
	if cmd.Use != "web" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "web")
	}
	if cmd.RunE == nil {
		t.Fatal("RunE is nil")
	}
}

func TestNew_RunE_FailsWithoutAPIKey(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")
	err := New().RunE(nil, nil)
	if err == nil {
		t.Fatal("expected error when GOOGLE_API_KEY is unset")
	}
	if !strings.Contains(err.Error(), "GOOGLE_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}
