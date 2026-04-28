//go:build !windows

package cursor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// installStub writes a `cursor-agent` shell script into t.TempDir(),
// prepends that dir to PATH, and returns the path of the file the
// script records its argv into. Args are separated by NUL bytes so
// arguments that themselves contain newlines (the multi-line planner
// prompt) round-trip cleanly. The stub emits stdout verbatim and exits
// with `exitCode` so callers can drive success/failure paths through
// the real os/exec layer.
func installStub(t *testing.T, stdout string, exitCode int) (callsPath string) {
	t.Helper()
	dir := t.TempDir()
	callsPath = filepath.Join(dir, "calls.log")
	stdoutPath := filepath.Join(dir, "stdout.txt")
	if err := os.WriteFile(stdoutPath, []byte(stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`#!/bin/sh
: > %q
for a in "$@"; do printf '%%s\0' "$a" >> %q; done
cat %q
exit %d
`, callsPath, callsPath, stdoutPath, exitCode)
	bin := filepath.Join(dir, Binary)
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return callsPath
}

// readCalls returns the argv recorded by installStub. An empty file
// yields a nil slice so callers can compare with reflect.DeepEqual.
func readCalls(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read calls.log: %v", err)
	}
	if len(b) == 0 {
		return nil
	}
	parts := strings.Split(string(b), "\x00")
	// printf appends a trailing NUL after the last arg, leaving an
	// empty tail entry in the split — discard it.
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func TestListModels(t *testing.T) {
	calls := installStub(t, sampleListModels, 0)

	got, err := New().ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := []string{"auto", "composer-2-fast", "composer-2", "gpt-5.3-codex-low"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if argv := readCalls(t, calls); !reflect.DeepEqual(argv, []string{"--list-models"}) {
		t.Fatalf("argv = %v", argv)
	}
}

func TestListModels_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	if _, err := New().ListModels(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestListModels_EmptyList(t *testing.T) {
	installStub(t, "Available models\n\n", 0)
	_, err := New().ListModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no models") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckLogin(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		exitCode int
		wantErr  bool
	}{
		{"logged-in", "Logged in as foo", 0, false},
		{"logged-in-mixed-case", "✓ login successful\nLogged In as foo", 0, false},
		{"logged-out", "Not logged in", 0, true},
		{"explicit-logged-out", "User is logged out", 0, true},
		{"not-authenticated", "Error: Not authenticated", 0, true},
		{"signed-out", "you are signed out", 0, true},
		{"unknown-output", "something unexpected", 0, true},
		{"runner-error", "", 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := installStub(t, tc.out, tc.exitCode)
			err := New().CheckLogin(context.Background())
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if argv := readCalls(t, calls); !reflect.DeepEqual(argv, []string{"status"}) {
				t.Fatalf("argv = %v", argv)
			}
		})
	}
}

func TestPlan_Interactive(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "spec.plan.md")
	calls := installStub(t, "", 0)

	err := New().Plan(context.Background(), codingagents.PlanRequest{
		TargetPath:  target,
		Body:        "# task\nbody",
		Model:       "composer-2-fast",
		OutputPath:  out,
		Interactive: true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	argv := readCalls(t, calls)
	want := []string{"--model", "composer-2-fast", "--workspace", dir}
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v", argv)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
	prompt := argv[len(argv)-1]
	if !strings.Contains(prompt, "# task") || !strings.Contains(prompt, target) {
		t.Fatalf("prompt missing task/target: %q", prompt)
	}
	if !strings.Contains(prompt, out) {
		t.Fatalf("prompt missing output path %q: %q", out, prompt)
	}
	if !strings.Contains(prompt, "save it to") {
		t.Fatalf("prompt missing save instruction: %q", prompt)
	}
}

func TestPlan_Interactive_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	err := New().Plan(context.Background(), codingagents.PlanRequest{
		TargetPath:  "/tmp/x.md",
		Body:        "x",
		Model:       "m",
		OutputPath:  "/tmp/x.plan.md",
		Interactive: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlan_Headless(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "spec.plan.md")
	calls := installStub(t, "  1. step one\n2. step two  \n", 0)

	err := New().Plan(context.Background(), codingagents.PlanRequest{
		TargetPath:  target,
		Body:        "# task\nbody",
		Model:       "sonnet-4",
		OutputPath:  out,
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	argv := readCalls(t, calls)
	want := []string{
		"--print",
		"--output-format", "text",
		"--mode", "plan",
		"--model", "sonnet-4",
		"--workspace", dir,
	}
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v", argv)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("plan file: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "1. step one\n2. step two" {
		t.Fatalf("plan body = %q", got)
	}
}

func TestPlan_Headless_EmptyOutput(t *testing.T) {
	installStub(t, "   \n  ", 0)
	err := New().Plan(context.Background(), codingagents.PlanRequest{
		TargetPath:  "/tmp/x.md",
		Body:        "x",
		Model:       "m",
		OutputPath:  "/tmp/x.plan.md",
		Interactive: false,
	})
	if err == nil || !strings.Contains(err.Error(), "empty plan") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlan_Headless_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	err := New().Plan(context.Background(), codingagents.PlanRequest{
		TargetPath:  "/tmp/x.md",
		Body:        "x",
		Model:       "m",
		OutputPath:  "/tmp/x.plan.md",
		Interactive: false,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlan_Scratch(t *testing.T) {
	calls := installStub(t, "", 0)

	err := New().Plan(context.Background(), codingagents.PlanRequest{
		Model:       "composer-2-fast",
		Interactive: true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{"--mode", "plan", "--model", "composer-2-fast"}
	if argv := readCalls(t, calls); !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
}

func TestPlan_Scratch_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	err := New().Plan(context.Background(), codingagents.PlanRequest{
		Model:       "m",
		Interactive: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlan_Headless_WriteError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	installStub(t, "step\n", 0)
	err := New().Plan(context.Background(), codingagents.PlanRequest{
		TargetPath:  filepath.Join(dir, "spec.md"),
		Body:        "x",
		Model:       "m",
		OutputPath:  filepath.Join(dir, "spec.plan.md"),
		Interactive: false,
	})
	if err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("err = %v", err)
	}
}
