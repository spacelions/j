//go:build !windows

package claude

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

// spawnWaitTimeout bounds the polling helpers below. The claude stub
// is a short shell script and finishes almost immediately, but a
// loaded CI machine can still spend several hundred milliseconds
// between cmd.Start() returning and the child writing its argv to
// disk. Five seconds is generous enough to avoid flakes without
// dragging out a happy-path test run.
const spawnWaitTimeout = 5 * time.Second

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

// installStub writes a `claude` shell script into t.TempDir(),
// prepends that dir to PATH, and returns the path of the file the
// script records its argv into. The stub also records its CWD so
// tests can assert cmd.Dir was honoured (claude has no --workspace
// flag; the workspace concept is mapped onto cmd.Dir by the backend).
// Args are NUL-separated so multi-line prompts round-trip cleanly.
func installStub(t *testing.T, stdout string, exitCode int) (callsPath, cwdPath string) {
	t.Helper()
	dir := t.TempDir()
	callsPath = filepath.Join(dir, "calls.log")
	cwdPath = filepath.Join(dir, "cwd.log")
	stdoutPath := filepath.Join(dir, "stdout.txt")
	if err := os.WriteFile(stdoutPath, []byte(stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`#!/bin/sh
: > %q
for a in "$@"; do printf '%%s\0' "$a" >> %q; done
pwd > %q
cat %q
exit %d
`, callsPath, callsPath, cwdPath, stdoutPath, exitCode)
	bin := filepath.Join(dir, Binary)
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return callsPath, cwdPath
}

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

// readCwd reads the working directory the stub script was invoked
// from. Used to assert cmd.Dir plumbing in the headless and
// interactive paths.
func readCwd(t *testing.T, cwdPath string) string {
	t.Helper()
	b, err := os.ReadFile(cwdPath)
	if err != nil {
		t.Fatalf("read cwd.log: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// waitForCwd polls cwdPath until it has content or the timeout fires.
// Used in headless paths where the child writes asynchronously.
func waitForCwd(t *testing.T, cwdPath string) string {
	t.Helper()
	deadline := time.Now().Add(spawnWaitTimeout)
	for {
		b, err := os.ReadFile(cwdPath)
		if err == nil && len(strings.TrimSpace(string(b))) > 0 {
			return strings.TrimSpace(string(b))
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for cwd at %s (err=%v)", cwdPath, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// assertCwd compares actual against want under filepath.EvalSymlinks
// so the macOS /var -> /private/var hop does not break the assertion.
func assertCwd(t *testing.T, want, got string) {
	t.Helper()
	wantResolved, err := filepath.EvalSymlinks(want)
	if err != nil {
		wantResolved = want
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		gotResolved = got
	}
	if wantResolved != gotResolved {
		t.Fatalf("cwd = %q, want %q", got, want)
	}
}

// TestCheckLogin_LoggedIn pins the happy path: `claude auth status`
// returns JSON with `loggedIn: true` and CheckLogin returns nil.
func TestCheckLogin_LoggedIn(t *testing.T) {
	calls, _ := installStub(t, `{"loggedIn": true, "authMethod": "claude.ai"}`, 0)
	if err := New().CheckLogin(context.Background()); err != nil {
		t.Fatalf("CheckLogin: %v", err)
	}
	if argv := readCalls(t, calls); !reflect.DeepEqual(argv, []string{"auth", "status"}) {
		t.Fatalf("argv = %v", argv)
	}
}

// TestCheckLogin_LoggedOut covers the JSON loggedIn=false branch.
func TestCheckLogin_LoggedOut(t *testing.T) {
	installStub(t, `{"loggedIn": false}`, 0)
	err := New().CheckLogin(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
}

// TestCheckLogin_BadJSON covers the unparseable-output branch: a
// human-readable string from `claude auth status --text` does not
// parse as JSON, so CheckLogin treats it as logged-out.
func TestCheckLogin_BadJSON(t *testing.T) {
	installStub(t, "not json at all", 0)
	err := New().CheckLogin(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
}

// TestCheckLogin_RunnerError covers the non-zero exit branch: the
// wrapped error mentions claude and the remediation hint.
func TestCheckLogin_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	err := New().CheckLogin(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "claude auth status failed") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "claude auth login") {
		t.Fatalf("err missing remediation hint: %v", err)
	}
}

// TestPlan_Interactive pins the interactive flow's argv shape and the
// embedded save instruction in the prompt. The interactive path
// passes `--permission-mode plan` (mirroring cursor's --mode plan).
// cmd.Dir is the per-task workspace (claude has no --workspace flag).
func TestPlan_Interactive(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	reqOut := filepath.Join(dir, "requirements.md")
	planOut := filepath.Join(dir, "plan.md")
	calls, cwdPath := installStub(t, "", 0)

	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "sonnet",
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
	want := []string{"--permission-mode", "plan", "--model", "sonnet"}
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
	if !strings.Contains(prompt, "one-line summary") {
		t.Fatalf("prompt missing one-line summary requirement: %q", prompt)
	}
	if !strings.Contains(prompt, "# Requirements") {
		t.Fatalf("prompt missing forbidden-heading reminder: %q", prompt)
	}
	assertCwd(t, dir, readCwd(t, cwdPath))
}

func TestPlan_Interactive_FirstRun_SessionID(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls, _ := installStub(t, "", 0)
	rid := "22222222-2222-4222-8222-222222222222"
	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "sonnet",
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
	want := []string{"--session-id", rid, "--permission-mode", "plan", "--model", "sonnet"}
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
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Plan pid = %d, want 0 on error", pid)
	}
}

// TestPlan_Headless pins the headless argv shape: --print
// --output-format text --dangerously-skip-permissions (no
// --permission-mode plan), plus the save-instruction prompt.
func TestPlan_Headless(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	reqOut := filepath.Join(dir, "requirements.md")
	planOut := filepath.Join(dir, "plan.md")
	logPath := filepath.Join(dir, "agent.log")
	calls, cwdPath := installStub(t, "ok\n", 0)

	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "sonnet",
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
		"--dangerously-skip-permissions",
		"--model", "sonnet",
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
	for _, banned := range []string{"--permission-mode", "--mode"} {
		for _, a := range argv {
			if a == banned {
				t.Fatalf("headless Plan should not pass %q: argv = %v", banned, argv)
			}
		}
	}
	prompt := argv[len(argv)-1]
	if !strings.Contains(prompt, reqOut) || !strings.Contains(prompt, planOut) || !strings.Contains(prompt, "Save") {
		t.Fatalf("prompt missing artefacts: %q", prompt)
	}
	waitForLog(t, logPath, "ok")
	assertCwd(t, dir, waitForCwd(t, cwdPath))
}

func TestPlan_Headless_FirstRun_SessionID(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls, _ := installStub(t, "ok\n", 0)
	rid := "33333333-3333-4333-8333-333333333333"
	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "sonnet",
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
		"--session-id", rid,
		"--print",
		"--output-format", "text",
		"--dangerously-skip-permissions",
		"--model", "sonnet",
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

// TestPlan_Headless_SpawnError exercises the SpawnIn-failure branch
// with the directory-as-log trick.
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
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Plan pid = %d, want 0 on Spawn error", pid)
	}
}

// TestPlan_Interactive_Resume pins the resume prompt path: argv
// carries --resume <id> (NOT --session-id), the prompt includes the
// resume markers and does not include the planner instruction body
// or the save suffix.
func TestPlan_Interactive_Resume(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls, _ := installStub(t, "", 0)
	rid := "66666666-6666-4666-8666-666666666666"
	pid, err := New().Plan(context.Background(), codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   "# task\nbody",
		Model:                  "sonnet",
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
	want := []string{"--resume", rid, "--permission-mode", "plan", "--model", "sonnet"}
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

func TestWork_Interactive(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("1. step one\n2. step two"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls, cwdPath := installStub(t, "", 0)

	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:    plan,
		Body:        "1. step one\n2. step two",
		Model:       "sonnet",
		Interactive: true,
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if pid != 0 {
		t.Fatalf("Work pid = %d, want 0 for interactive", pid)
	}
	argv := readCalls(t, calls)
	want := []string{"--model", "sonnet"}
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
	for _, banned := range []string{"--print", "--permission-mode", "--output-format", "--dangerously-skip-permissions"} {
		for _, a := range argv {
			if a == banned {
				t.Fatalf("interactive Work should not pass %q: argv = %v", banned, argv)
			}
		}
	}
	assertCwd(t, dir, readCwd(t, cwdPath))
}

func TestWork_Interactive_Resume(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("1. step one\n2. step two"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls, _ := installStub(t, "", 0)
	rid := "77777777-7777-4777-8777-777777777777"
	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:     plan,
		Body:         "1. step one\n2. step two",
		Model:        "sonnet",
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
	want := []string{"--resume", rid, "--model", "sonnet"}
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
}

func TestWork_Headless(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("plan body"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "agent.log")
	calls, cwdPath := installStub(t, "ok\n", 0)

	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:     plan,
		Body:         "plan body",
		Model:        "sonnet",
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
		"--dangerously-skip-permissions",
		"--model", "sonnet",
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
	for _, banned := range []string{"--permission-mode", "--mode"} {
		for _, a := range argv {
			if a == banned {
				t.Fatalf("headless Work should not pass %q: argv = %v", banned, argv)
			}
		}
	}
	waitForLog(t, logPath, "ok")
	assertCwd(t, dir, waitForCwd(t, cwdPath))
}

func TestWork_Headless_Resume(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("plan body"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls, _ := installStub(t, "ok\n", 0)
	rid := "55555555-5555-4555-8555-555555555555"
	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:     plan,
		Body:         "plan body",
		Model:        "sonnet",
		Interactive:  false,
		ResumeChatID: rid,
		Resume:       true,
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
		"--dangerously-skip-permissions",
		"--model", "sonnet",
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
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Work pid = %d, want 0 on error", pid)
	}
}

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
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Work pid = %d, want 0 on Spawn error", pid)
	}
}

// TestWork_Interactive_FixFindings pins the fix-findings branch in
// buildWorkPrompt: a non-empty FixFindings switches to BuildVerifierFix
// and the prompt embeds the findings + forbids re-planning.
func TestWork_Interactive_FixFindings(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("1. step one"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls, _ := installStub(t, "", 0)

	pid, err := New().Work(context.Background(), codingagents.WorkRequest{
		PlanPath:    plan,
		Body:        "1. step one",
		Model:       "sonnet",
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

// TestWork_FixFindings_BeatsResume pins precedence in buildWorkPrompt:
// when both FixFindings and Resume are set, the fix branch wins.
func TestWork_FixFindings_BeatsResume(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(plan, []byte("1. step"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls, _ := installStub(t, "", 0)
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
}

// TestVerify_Interactive pins the interactive flow's argv shape and
// embedded verifier prompt. cmd.Dir is the project root (claude has
// no --workspace flag); the test chdirs into dir and asserts the
// stub was invoked from the canonicalised cwd.
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
	calls, cwdPath := installStub(t, "", 0)

	pid, err := New().Verify(context.Background(), codingagents.VerifyRequest{
		RequirementsPath:           reqPath,
		RequirementsBody:           "# req\nbody",
		PlanPath:                   planPath,
		PlanBody:                   "1. step",
		VerifierPlanOutputPath:     verifierPlan,
		VerifierFindingsOutputPath: findingsPath,
		Model:                      "sonnet",
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
	want := []string{"--model", "sonnet"}
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v", argv)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
	for _, banned := range []string{"--print", "--permission-mode", "--output-format", "--dangerously-skip-permissions"} {
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
	for _, want := range []string{reqPath, "# req", planPath, "1. step", verifierPlan, findingsPath, "VERDICT: PASS", "VERDICT: FAIL", "j-verify-task", "git worktree list"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
	assertCwd(t, cwd, readCwd(t, cwdPath))
}

func TestVerify_Interactive_Resume(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	findingsPath := filepath.Join(dir, "verifier_findings.md")
	calls, _ := installStub(t, "", 0)
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
	if strings.Contains(prompt, strings.TrimSpace(verifier.Instruction)) {
		t.Fatalf("resume prompt should not include verifier.Instruction: %q", prompt)
	}
	for _, banned := range []string{"Save", "Then exit."} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("resume prompt should not include %q: %q", banned, prompt)
		}
	}
}

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
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Verify pid = %d, want 0 on error", pid)
	}
}

func TestVerify_Headless(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	findingsPath := filepath.Join(dir, "verifier_findings.md")
	logPath := filepath.Join(dir, "agent.log")
	calls, cwdPath := installStub(t, "ok\n", 0)

	pid, err := New().Verify(context.Background(), codingagents.VerifyRequest{
		RequirementsPath:           filepath.Join(dir, "requirements.md"),
		RequirementsBody:           "# req",
		PlanPath:                   filepath.Join(dir, "plan.md"),
		PlanBody:                   "plan body",
		VerifierPlanOutputPath:     filepath.Join(dir, "verifier_plan.md"),
		VerifierFindingsOutputPath: findingsPath,
		Model:                      "sonnet",
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
		"--dangerously-skip-permissions",
		"--model", "sonnet",
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
	for _, banned := range []string{"--permission-mode", "--mode"} {
		for _, a := range argv {
			if a == banned {
				t.Fatalf("headless Verify should not pass %q: argv = %v", banned, argv)
			}
		}
	}
	waitForLog(t, logPath, "ok")
	assertCwd(t, cwd, waitForCwd(t, cwdPath))
}

func TestVerify_Headless_Resume(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	findingsPath := filepath.Join(dir, "verifier_findings.md")
	calls, _ := installStub(t, "ok\n", 0)
	rid := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

	pid, err := New().Verify(context.Background(), codingagents.VerifyRequest{
		RequirementsPath:           filepath.Join(dir, "requirements.md"),
		PlanPath:                   filepath.Join(dir, "plan.md"),
		VerifierPlanOutputPath:     filepath.Join(dir, "verifier_plan.md"),
		VerifierFindingsOutputPath: findingsPath,
		Model:                      "sonnet",
		Interactive:                false,
		ResumeChatID:               rid,
		Resume:                     true,
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
		"--dangerously-skip-permissions",
		"--model", "sonnet",
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
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("Verify pid = %d, want 0 on Spawn error", pid)
	}
}
