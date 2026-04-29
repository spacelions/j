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

func TestCreateChatID(t *testing.T) {
	calls := installStub(t, "  2b43f90a-b742-4d4b-9f0c-e1ee8ad43f83  \n", 0)
	id, err := CreateChatID(context.Background())
	if err != nil {
		t.Fatalf("CreateChatID: %v", err)
	}
	if id != "2b43f90a-b742-4d4b-9f0c-e1ee8ad43f83" {
		t.Fatalf("id = %q", id)
	}
	if got := readCalls(t, calls); !reflect.DeepEqual(got, []string{"create-chat"}) {
		t.Fatalf("argv = %v", got)
	}
}

func TestCreateChatID_EmptyOutput(t *testing.T) {
	installStub(t, "  \n  \t  ", 0)
	_, err := CreateChatID(context.Background())
	if err == nil || !strings.Contains(err.Error(), "empty id") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateChatID_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	_, err := CreateChatID(context.Background())
	if err == nil || !strings.Contains(err.Error(), "create-chat") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewResumeID_DelegatesToCreateChat(t *testing.T) {
	calls := installStub(t, "  abc-id  \n", 0)
	id, err := New().NewResumeID(context.Background())
	if err != nil {
		t.Fatalf("NewResumeID: %v", err)
	}
	if id != "abc-id" {
		t.Fatalf("id = %q, want abc-id", id)
	}
	if got := readCalls(t, calls); !reflect.DeepEqual(got, []string{"create-chat"}) {
		t.Fatalf("argv = %v", got)
	}
}

func TestNewResumeID_PropagatesError(t *testing.T) {
	installStub(t, "", 1)
	if _, err := New().NewResumeID(context.Background()); err == nil {
		t.Fatal("expected error")
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

// TestPlan_Interactive pins the interactive flow's argv shape and the
// embedded save instruction in the prompt: the agent is responsible
// for writing both requirements.md and plan.md.
func TestPlan_Interactive(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	reqOut := filepath.Join(dir, "requirements.md")
	planOut := filepath.Join(dir, "plan.md")
	calls := installStub(t, "", 0)

	err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "composer-2-fast",
		RequirementsOutputPath: reqOut,
		PlanOutputPath:         planOut,
		Interactive:            true,
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
	if !strings.Contains(prompt, reqOut) {
		t.Fatalf("prompt missing requirements path %q: %q", reqOut, prompt)
	}
	if !strings.Contains(prompt, planOut) {
		t.Fatalf("prompt missing plan path %q: %q", planOut, prompt)
	}
	if !strings.Contains(prompt, "Save") {
		t.Fatalf("prompt missing save instruction: %q", prompt)
	}
}

func TestPlan_Interactive_ResumeChatID(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "", 0)
	rid := "22222222-2222-4222-8222-222222222222"
	err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "composer-2-fast",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            true,
		ResumeChatID:           rid,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	argv := readCalls(t, calls)
	want := []string{"--resume", rid, "--model", "composer-2-fast", "--workspace", dir}
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v", argv)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
}

func TestPlan_Interactive_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           "/tmp/x.md",
		Body:                   "x",
		Model:                  "m",
		RequirementsOutputPath: "/tmp/requirements.md",
		PlanOutputPath:         "/tmp/plan.md",
		Interactive:            true,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
}

// TestPlan_Headless pins the headless argv shape: --print
// --output-format text --mode plan, plus the save-instruction prompt.
// The agent is responsible for writing the files; the orchestrator
// only consumes the captured stdout for warnings.
func TestPlan_Headless(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "ok\n", 0)

	err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "sonnet-4",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            false,
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
}

func TestPlan_Headless_ResumeChatID(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "ok\n", 0)
	rid := "33333333-3333-4333-8333-333333333333"
	err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "sonnet-4",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            false,
		ResumeChatID:           rid,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	argv := readCalls(t, calls)
	want := []string{
		"--resume", rid,
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
}

func TestPlan_Headless_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           "/tmp/x.md",
		Body:                   "x",
		Model:                  "m",
		RequirementsOutputPath: "/tmp/requirements.md",
		PlanOutputPath:         "/tmp/plan.md",
		Interactive:            false,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
}

func TestWork_Interactive(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("1. step one\n2. step two"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "", 0)

	err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:    plan,
		Body:        "1. step one\n2. step two",
		Model:       "composer-2-fast",
		Interactive: true,
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
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
	if !strings.Contains(prompt, "1. step one") {
		t.Fatalf("prompt missing plan body: %q", prompt)
	}
	if !strings.Contains(prompt, plan) {
		t.Fatalf("prompt missing plan path %q: %q", plan, prompt)
	}
	for _, banned := range []string{"--print", "--mode", "--output-format"} {
		for _, a := range argv {
			if a == banned {
				t.Fatalf("interactive Work should not pass %q: argv = %v", banned, argv)
			}
		}
	}
}

func TestWork_Interactive_ResumeChatID(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("1. step one\n2. step two"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "", 0)
	rid := "44444444-4444-4444-8444-444444444444"
	err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:     plan,
		Body:         "1. step one\n2. step two",
		Model:        "composer-2-fast",
		Interactive:  true,
		ResumeChatID: rid,
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	argv := readCalls(t, calls)
	want := []string{"--resume", rid, "--model", "composer-2-fast", "--workspace", dir}
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v", argv)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
}

func TestWork_Headless(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("plan body"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "ok\n", 0)

	err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:    plan,
		Body:        "plan body",
		Model:       "sonnet-4",
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	argv := readCalls(t, calls)
	want := []string{
		"--print",
		"--output-format", "text",
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
	for _, a := range argv {
		if a == "--mode" {
			t.Fatalf("headless Work should not pass --mode: argv = %v", argv)
		}
	}
}

func TestWork_Headless_ResumeChatID(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("plan body"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "ok\n", 0)
	rid := "55555555-5555-4555-8555-555555555555"
	err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:     plan,
		Body:         "plan body",
		Model:        "sonnet-4",
		Interactive:  false,
		ResumeChatID: rid,
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	argv := readCalls(t, calls)
	want := []string{
		"--resume", rid,
		"--print",
		"--output-format", "text",
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
}

func TestWork_Interactive_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:    "/tmp/x.plan.md",
		Body:        "x",
		Model:       "m",
		Interactive: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
}

func TestWork_Headless_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:    "/tmp/x.plan.md",
		Body:        "x",
		Model:       "m",
		Interactive: false,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
}
