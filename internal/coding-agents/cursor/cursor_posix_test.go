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
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/workflow/agents/coder"
	"github.com/spacelions/j/internal/workflow/agents/planner"
	"github.com/spacelions/j/internal/workflow/agents/verifier"
)

// spawnWaitTimeout bounds the polling helpers below. The cursor stub
// is a short shell script and finishes almost immediately, but a
// loaded CI machine can still spend several hundred milliseconds
// between cmd.Start() returning and the child writing its argv to
// disk. Five seconds is generous enough to avoid flakes without
// dragging out a happy-path test run.
const spawnWaitTimeout = 5 * time.Second

// waitForCalls polls callsPath until at least want argv entries are
// recorded, or the timeout fires. Spawn returns immediately after
// fork/exec so the child may still be writing its argv when the
// caller reaches the assertion; this helper bridges that gap without
// reintroducing a synchronous Wait.
func waitForCalls(t *testing.T, callsPath string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(spawnWaitTimeout)
	for {
		argv := readCallsBestEffort(callsPath)
		if len(argv) >= want {
			return argv
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %d argv entries at %s, got %d: %v",
				want, callsPath, len(argv), argv)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// readCallsBestEffort returns the argv recorded so far at path, with
// every read error swallowed so the polling loop in waitForCalls can
// retry. Treats both "file does not exist" and "empty file" as
// "child has not finished yet" and yields a nil slice.
func readCallsBestEffort(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
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

// waitForLog polls logPath until want is found in its contents or the
// timeout fires. Used to assert the spawned child's stdout/stderr
// landed in the per-task agent log.
func waitForLog(t *testing.T, logPath, want string) string {
	t.Helper()
	deadline := time.Now().Add(spawnWaitTimeout)
	for {
		data, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(data), want) {
			return string(data)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %q in %s; last contents %q (err=%v)",
				want, logPath, data, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

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
// for writing both requirements.md and plan.md. The interactive path
// also passes `--mode plan` (alongside the headless path) so users get
// the same plan-mode behaviour in the TUI. The interactive Plan stays
// synchronous and returns pid == 0.
func TestPlan_Interactive(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	reqOut := filepath.Join(dir, "requirements.md")
	planOut := filepath.Join(dir, "plan.md")
	calls := installStub(t, "", 0)

	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
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
	if pid != 0 {
		t.Fatalf("Plan pid = %d, want 0 for interactive", pid)
	}
	argv := readCalls(t, calls)
	want := []string{"--mode", "plan", "--model", "composer-2-fast", "--workspace", dir}
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
	// AC#4 / AC#5: the non-resume save instruction must require
	// requirements.md to begin with a one-line summary and forbid
	// the literal `# Requirements` heading on line 1.
	if !strings.Contains(prompt, "one-line summary") {
		t.Fatalf("prompt missing one-line summary requirement: %q", prompt)
	}
	if !strings.Contains(prompt, "# Requirements") {
		t.Fatalf("prompt missing forbidden-heading reminder: %q", prompt)
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
	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
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
	if pid != 0 {
		t.Fatalf("Plan pid = %d, want 0 for interactive", pid)
	}
	argv := readCalls(t, calls)
	want := []string{"--resume", rid, "--mode", "plan", "--model", "composer-2-fast", "--workspace", dir}
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
	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
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
	if pid != 0 {
		t.Fatalf("Plan pid = %d, want 0 on error", pid)
	}
}

// TestPlan_Headless pins the headless argv shape: --print
// --output-format text --force --trust (no --mode plan), plus the
// save-instruction prompt. The headless flow is fire-and-forget: the
// returned pid is positive, the helper does not wait, and the
// child's stdout/stderr land in the agent log file passed via
// PlanRequest.AgentLogPath. The same prompt suffix tells cursor to
// write the artifacts itself; --mode plan is intentionally absent
// because it would block those writes in headless mode.
func TestPlan_Headless(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	reqOut := filepath.Join(dir, "requirements.md")
	planOut := filepath.Join(dir, "plan.md")
	logPath := filepath.Join(dir, "agent.log")
	calls := installStub(t, "ok\n", 0)

	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "sonnet-4",
		RequirementsOutputPath: reqOut,
		PlanOutputPath:         planOut,
		Interactive:            false,
		AgentLogPath:           logPath,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("Plan pid = %d, want > 0 for headless background spawn", pid)
	}
	want := []string{
		"--print",
		"--output-format", "text",
		"--force",
		"--trust",
		"--model", "sonnet-4",
		"--workspace", dir,
	}
	argv := waitForCalls(t, calls, len(want)+1)
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
			t.Fatalf("headless Plan should not pass --mode: argv = %v", argv)
		}
	}
	prompt := argv[len(argv)-1]
	if !strings.Contains(prompt, reqOut) {
		t.Fatalf("prompt missing requirements path %q: %q", reqOut, prompt)
	}
	if !strings.Contains(prompt, planOut) {
		t.Fatalf("prompt missing plan path %q: %q", planOut, prompt)
	}
	if !strings.Contains(prompt, "Save") {
		t.Fatalf("prompt missing save instruction: %q", prompt)
	}
	waitForLog(t, logPath, "ok")
}

func TestPlan_Headless_ResumeChatID(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "ok\n", 0)
	rid := "33333333-3333-4333-8333-333333333333"
	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "sonnet-4",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            false,
		ResumeChatID:           rid,
		AgentLogPath:           filepath.Join(dir, "agent.log"),
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("Plan pid = %d, want > 0", pid)
	}
	want := []string{
		"--resume", rid,
		"--print",
		"--output-format", "text",
		"--force",
		"--trust",
		"--model", "sonnet-4",
		"--workspace", dir,
	}
	argv := waitForCalls(t, calls, len(want)+1)
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v", argv)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
}

// TestPlan_Headless_SpawnError exercises the Spawn-failure branch in
// the headless Plan path: AgentLogPath points at an existing
// directory, which OpenFile rejects, so Spawn returns an error
// before fork/exec. The wrapped "cursor-agent: ..." message is
// preserved so the orchestrator surfaces the same shape it always
// did when the headless runner failed. With Spawn replacing
// run.Output the child's exit code is no longer observable by the
// parent, so an installStub-with-exit-1 cannot drive an error here;
// the directory-as-log scenario stands in.
func TestPlan_Headless_SpawnError(t *testing.T) {
	installStub(t, "", 0)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	if err := os.MkdirAll(logPath, 0o755); err != nil {
		t.Fatal(err)
	}
	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           "/tmp/x.md",
		Body:                   "x",
		Model:                  "m",
		RequirementsOutputPath: "/tmp/requirements.md",
		PlanOutputPath:         "/tmp/plan.md",
		Interactive:            false,
		AgentLogPath:           logPath,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Plan pid = %d, want 0 on Spawn error", pid)
	}
}

func TestWork_Interactive(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("1. step one\n2. step two"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "", 0)

	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:    plan,
		Body:        "1. step one\n2. step two",
		Model:       "composer-2-fast",
		Interactive: true,
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if pid != 0 {
		t.Fatalf("Work pid = %d, want 0 for interactive", pid)
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
	for _, banned := range []string{"--print", "--mode", "--output-format", "--force", "--trust"} {
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
	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:     plan,
		Body:         "1. step one\n2. step two",
		Model:        "composer-2-fast",
		Interactive:  true,
		ResumeChatID: rid,
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if pid != 0 {
		t.Fatalf("Work pid = %d, want 0 for interactive", pid)
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
	logPath := filepath.Join(dir, "agent.log")
	calls := installStub(t, "ok\n", 0)

	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:     plan,
		Body:         "plan body",
		Model:        "sonnet-4",
		Interactive:  false,
		AgentLogPath: logPath,
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("Work pid = %d, want > 0 for headless background spawn", pid)
	}
	want := []string{
		"--print",
		"--output-format", "text",
		"--force",
		"--trust",
		"--model", "sonnet-4",
		"--workspace", dir,
	}
	argv := waitForCalls(t, calls, len(want)+1)
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
	waitForLog(t, logPath, "ok")
}

func TestWork_Headless_ResumeChatID(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("plan body"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "ok\n", 0)
	rid := "55555555-5555-4555-8555-555555555555"
	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:     plan,
		Body:         "plan body",
		Model:        "sonnet-4",
		Interactive:  false,
		ResumeChatID: rid,
		AgentLogPath: filepath.Join(dir, "agent.log"),
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("Work pid = %d, want > 0", pid)
	}
	want := []string{
		"--resume", rid,
		"--print",
		"--output-format", "text",
		"--force",
		"--trust",
		"--model", "sonnet-4",
		"--workspace", dir,
	}
	argv := waitForCalls(t, calls, len(want)+1)
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
	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:    "/tmp/x.plan.md",
		Body:        "x",
		Model:       "m",
		Interactive: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Work pid = %d, want 0 on error", pid)
	}
}

// TestWork_Headless_SpawnError covers the Spawn-failure branch in the
// headless Work path with the same directory-as-log trick as
// TestPlan_Headless_SpawnError. With Spawn replacing run.Output the
// child's exit code is no longer observable, so a stub-exit-1 cannot
// drive an error and the directory-as-log scenario stands in.
func TestWork_Headless_SpawnError(t *testing.T) {
	installStub(t, "", 0)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	if err := os.MkdirAll(logPath, 0o755); err != nil {
		t.Fatal(err)
	}
	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:     "/tmp/x.plan.md",
		Body:         "x",
		Model:        "m",
		Interactive:  false,
		AgentLogPath: logPath,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Work pid = %d, want 0 on Spawn error", pid)
	}
}

// TestPlan_Interactive_Resume pins AC#2 / AC#5c for the planner side.
// With Resume=true and a ResumeChatID set, argv must carry
// `--resume <id>`, the prompt must include the resume-marker words
// (previous / check / continue), and it must NOT include either the
// non-resume planner instruction body or the
// `Save ... Then exit.` save suffix.
func TestPlan_Interactive_Resume(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "", 0)
	rid := "66666666-6666-4666-8666-666666666666"
	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "composer-2-fast",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            true,
		ResumeChatID:           rid,
		Resume:                 true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid != 0 {
		t.Fatalf("Plan pid = %d, want 0 for interactive", pid)
	}
	argv := readCalls(t, calls)
	want := []string{"--resume", rid, "--mode", "plan", "--model", "composer-2-fast", "--workspace", dir}
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v", argv)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
	prompt := argv[len(argv)-1]
	lower := strings.ToLower(prompt)
	for _, marker := range []string{"previous", "check", "continue"} {
		if !strings.Contains(lower, marker) {
			t.Fatalf("resume prompt missing %q: %q", marker, prompt)
		}
	}
	if strings.Contains(prompt, strings.TrimSpace(planner.Instruction)) {
		t.Fatalf("resume prompt should not include planner.Instruction: %q", prompt)
	}
	for _, banned := range []string{"Save", "Then exit."} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("resume prompt should not include %q: %q", banned, prompt)
		}
	}
}

// TestWork_Interactive_FixFindings pins the fix-findings branch in
// buildWorkPrompt: a non-empty WorkRequest.FixFindings switches the
// composed prompt to BuildVerifierFix, which embeds the supplied
// findings body alongside the plan and explicitly forbids
// re-planning. The argv shape is unchanged from a regular
// interactive Work call.
func TestWork_Interactive_FixFindings(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("1. step one"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "", 0)

	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:    plan,
		Body:        "1. step one",
		Model:       "composer-2-fast",
		Interactive: true,
		FixFindings: "- missing tests for X\nVERDICT: FAIL",
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if pid != 0 {
		t.Fatalf("Work pid = %d, want 0 for interactive", pid)
	}
	argv := readCalls(t, calls)
	prompt := argv[len(argv)-1]
	for _, want := range []string{"missing tests for X", "VERDICT: FAIL", plan, "verifier_findings.md"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("fix prompt missing %q: %q", want, prompt)
		}
	}
	if strings.Contains(prompt, strings.TrimSpace(coder.Instruction)) {
		t.Fatalf("fix prompt should not include coder.Instruction: %q", prompt)
	}
	if !strings.Contains(prompt, "Do not re-plan") {
		t.Fatalf("fix prompt missing re-plan guard: %q", prompt)
	}
}

// TestWork_FixFindings_BeatsResume pins precedence in
// buildWorkPrompt: when both FixFindings and Resume are set, the
// fix branch wins so the coder receives the actionable findings
// rather than a generic resume cue.
func TestWork_FixFindings_BeatsResume(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("1. step"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "", 0)
	_, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:    plan,
		Body:        "1. step",
		Model:       "m",
		Interactive: true,
		Resume:      true,
		FixFindings: "- specific finding",
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	argv := readCalls(t, calls)
	prompt := argv[len(argv)-1]
	if !strings.Contains(prompt, "specific finding") {
		t.Fatalf("fix prompt missing findings body: %q", prompt)
	}
	if strings.Contains(strings.ToLower(prompt), "resuming a previous coding session.") &&
		!strings.Contains(prompt, "VERDICT: FAIL") {
		t.Fatalf("expected fix prompt, got resume prompt: %q", prompt)
	}
}

// TestVerify_Interactive pins the interactive flow's argv shape and
// embedded prompt for `j verify`. The verifier.Instruction body must
// be embedded along with the requirements / plan / output paths;
// --mode plan is intentionally absent because the verifier needs to
// edit verifier_*.md and (on FAIL) project files.
//
// Unlike Plan/Work, Verify runs with `--workspace <project-root>` so
// the verifier can `git worktree list` from the repo root; the test
// chdirs into dir before the call and asserts the workspace lands on
// the canonicalised cwd (which equals dir on most systems, modulo
// symlink resolution on macOS where /var -> /private/var).
func TestVerify_Interactive(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	reqPath := filepath.Join(dir, "requirements.md")
	if err := os.WriteFile(reqPath, []byte("# req\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("1. step"), 0o600); err != nil {
		t.Fatal(err)
	}
	verifierPlan := filepath.Join(dir, "verifier_plan.md")
	findingsPath := filepath.Join(dir, "verifier_findings.md")
	calls := installStub(t, "", 0)

	pid, err := New().Verify(context.Background(), codingagents.VerifyRequest{
		RequirementsPath:           reqPath,
		RequirementsBody:           "# req\nbody",
		PlanPath:                   planPath,
		PlanBody:                   "1. step",
		VerifierPlanOutputPath:     verifierPlan,
		VerifierFindingsOutputPath: findingsPath,
		Model:                      "composer-2-fast",
		Interactive:                true,
		Worktree:                   "j-verify-task",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if pid != 0 {
		t.Fatalf("Verify pid = %d, want 0 for interactive", pid)
	}
	argv := readCalls(t, calls)
	want := []string{"--model", "composer-2-fast", "--workspace", cwd}
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v", argv)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
	for _, banned := range []string{"--print", "--mode", "--output-format", "--force", "--trust"} {
		for _, a := range argv {
			if a == banned {
				t.Fatalf("interactive Verify should not pass %q: argv = %v", banned, argv)
			}
		}
	}
	prompt := argv[len(argv)-1]
	if !strings.Contains(prompt, strings.TrimSpace(verifier.Instruction)) {
		t.Fatalf("prompt missing verifier.Instruction: %q", prompt)
	}
	for _, want := range []string{reqPath, "# req", planPath, "1. step", findingsPath, "VERDICT: PASS", "VERDICT: FAIL", "j-verify-task", "git worktree list"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
	if strings.Contains(prompt, verifierPlan) {
		t.Fatalf("prompt should not reference verifier_plan.md: %q", prompt)
	}
}

// TestVerify_Interactive_ResumeChatID pins the --resume <id> arg
// shape on the interactive Verify path, mirroring
// TestPlan_Interactive_ResumeChatID / TestWork_Interactive_ResumeChatID.
// Workspace is asserted against the canonicalised cwd, same rule as
// TestVerify_Interactive.
func TestVerify_Interactive_ResumeChatID(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	findingsPath := filepath.Join(dir, "verifier_findings.md")
	calls := installStub(t, "", 0)
	rid := "88888888-8888-4888-8888-888888888888"

	pid, err := New().Verify(context.Background(), codingagents.VerifyRequest{
		RequirementsPath:           filepath.Join(dir, "requirements.md"),
		PlanPath:                   filepath.Join(dir, "plan.md"),
		VerifierPlanOutputPath:     filepath.Join(dir, "verifier_plan.md"),
		VerifierFindingsOutputPath: findingsPath,
		Model:                      "composer-2-fast",
		Interactive:                true,
		ResumeChatID:               rid,
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if pid != 0 {
		t.Fatalf("Verify pid = %d, want 0 for interactive", pid)
	}
	argv := readCalls(t, calls)
	want := []string{"--resume", rid, "--model", "composer-2-fast", "--workspace", cwd}
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v", argv)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
}

// TestVerify_Interactive_Resume pins the resume prompt path: argv
// carries --resume <id>, the prompt mentions previous/check/continue
// and does NOT include verifier.Instruction or the save-suffix.
func TestVerify_Interactive_Resume(t *testing.T) {
	dir := t.TempDir()
	findingsPath := filepath.Join(dir, "verifier_findings.md")
	calls := installStub(t, "", 0)
	rid := "99999999-9999-4999-8999-999999999999"

	_, err := New().Verify(context.Background(), codingagents.VerifyRequest{
		RequirementsPath:           filepath.Join(dir, "requirements.md"),
		RequirementsBody:           "# req",
		PlanPath:                   filepath.Join(dir, "plan.md"),
		PlanBody:                   "plan body",
		VerifierPlanOutputPath:     filepath.Join(dir, "verifier_plan.md"),
		VerifierFindingsOutputPath: findingsPath,
		Model:                      "m",
		Interactive:                true,
		ResumeChatID:               rid,
		Resume:                     true,
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	argv := readCalls(t, calls)
	if argv[0] != "--resume" || argv[1] != rid {
		t.Fatalf("argv missing --resume %q: %v", rid, argv)
	}
	prompt := argv[len(argv)-1]
	lower := strings.ToLower(prompt)
	for _, marker := range []string{"previous", "check", "continue"} {
		if !strings.Contains(lower, marker) {
			t.Fatalf("resume prompt missing %q: %q", marker, prompt)
		}
	}
	if strings.Contains(prompt, strings.TrimSpace(verifier.Instruction)) {
		t.Fatalf("resume prompt should not include verifier.Instruction: %q", prompt)
	}
	for _, banned := range []string{"Save", "Then exit."} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("resume prompt should not include %q: %q", banned, prompt)
		}
	}
}

// TestVerify_Interactive_RunnerError exercises the run.Run error
// path in Verify (interactive branch), mirroring
// TestPlan_Interactive_RunnerError / TestWork_Interactive_RunnerError.
func TestVerify_Interactive_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	pid, err := New().Verify(context.Background(), codingagents.VerifyRequest{
		RequirementsPath:           "/tmp/req.md",
		PlanPath:                   "/tmp/plan.md",
		VerifierPlanOutputPath:     "/tmp/verifier_plan.md",
		VerifierFindingsOutputPath: "/tmp/verifier_findings.md",
		Model:                      "m",
		Interactive:                true,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Verify pid = %d, want 0 on error", pid)
	}
}

// TestVerify_Headless pins the headless flag set: --print
// --output-format text --force --trust, the workspace, the model,
// and the prompt suffix. --mode plan is intentionally absent.
// Workspace is the canonicalised cwd (set via t.Chdir), not the
// per-task dir: `j verify` must run from the project root.
func TestVerify_Headless(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	findingsPath := filepath.Join(dir, "verifier_findings.md")
	logPath := filepath.Join(dir, "agent.log")
	calls := installStub(t, "ok\n", 0)

	pid, err := New().Verify(context.Background(), codingagents.VerifyRequest{
		RequirementsPath:           filepath.Join(dir, "requirements.md"),
		RequirementsBody:           "# req",
		PlanPath:                   filepath.Join(dir, "plan.md"),
		PlanBody:                   "plan body",
		VerifierPlanOutputPath:     filepath.Join(dir, "verifier_plan.md"),
		VerifierFindingsOutputPath: findingsPath,
		Model:                      "sonnet-4",
		Interactive:                false,
		AgentLogPath:               logPath,
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("Verify pid = %d, want > 0 for headless background spawn", pid)
	}
	want := []string{
		"--print",
		"--output-format", "text",
		"--force",
		"--trust",
		"--model", "sonnet-4",
		"--workspace", cwd,
	}
	argv := waitForCalls(t, calls, len(want)+1)
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
			t.Fatalf("headless Verify should not pass --mode: argv = %v", argv)
		}
	}
	waitForLog(t, logPath, "ok")
}

// TestVerify_Headless_ResumeChatID pins the --resume <id> argv shape
// on the headless Verify path. Workspace is the canonicalised cwd,
// same rule as TestVerify_Headless.
func TestVerify_Headless_ResumeChatID(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	findingsPath := filepath.Join(dir, "verifier_findings.md")
	calls := installStub(t, "ok\n", 0)
	rid := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

	pid, err := New().Verify(context.Background(), codingagents.VerifyRequest{
		RequirementsPath:           filepath.Join(dir, "requirements.md"),
		PlanPath:                   filepath.Join(dir, "plan.md"),
		VerifierPlanOutputPath:     filepath.Join(dir, "verifier_plan.md"),
		VerifierFindingsOutputPath: findingsPath,
		Model:                      "sonnet-4",
		Interactive:                false,
		ResumeChatID:               rid,
		AgentLogPath:               filepath.Join(dir, "agent.log"),
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("Verify pid = %d, want > 0", pid)
	}
	want := []string{
		"--resume", rid,
		"--print",
		"--output-format", "text",
		"--force",
		"--trust",
		"--model", "sonnet-4",
		"--workspace", cwd,
	}
	argv := waitForCalls(t, calls, len(want)+1)
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v", argv)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
}

// TestVerify_Headless_SpawnError exercises the Spawn-failure branch
// on the headless Verify path with the directory-as-log trick.
func TestVerify_Headless_SpawnError(t *testing.T) {
	installStub(t, "", 0)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	if err := os.MkdirAll(logPath, 0o755); err != nil {
		t.Fatal(err)
	}
	pid, err := New().Verify(context.Background(), codingagents.VerifyRequest{
		RequirementsPath:           "/tmp/req.md",
		PlanPath:                   "/tmp/plan.md",
		VerifierPlanOutputPath:     "/tmp/verifier_plan.md",
		VerifierFindingsOutputPath: "/tmp/verifier_findings.md",
		Model:                      "m",
		Interactive:                false,
		AgentLogPath:               logPath,
	})
	if err == nil || !strings.Contains(err.Error(), "cursor-agent") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Verify pid = %d, want 0 on Spawn error", pid)
	}
}

// TestWork_Interactive_Resume mirrors TestPlan_Interactive_Resume for
// the coder side (AC#2 / AC#5c).
func TestWork_Interactive_Resume(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("1. step one\n2. step two"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := installStub(t, "", 0)
	rid := "77777777-7777-4777-8777-777777777777"
	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:     plan,
		Body:         "1. step one\n2. step two",
		Model:        "composer-2-fast",
		Interactive:  true,
		ResumeChatID: rid,
		Resume:       true,
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if pid != 0 {
		t.Fatalf("Work pid = %d, want 0 for interactive", pid)
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
	prompt := argv[len(argv)-1]
	lower := strings.ToLower(prompt)
	for _, marker := range []string{"previous", "check", "continue"} {
		if !strings.Contains(lower, marker) {
			t.Fatalf("resume prompt missing %q: %q", marker, prompt)
		}
	}
	if strings.Contains(prompt, strings.TrimSpace(coder.Instruction)) {
		t.Fatalf("resume prompt should not include coder.Instruction: %q", prompt)
	}
	for _, banned := range []string{"Save", "Then exit."} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("resume prompt should not include %q: %q", banned, prompt)
		}
	}
}
