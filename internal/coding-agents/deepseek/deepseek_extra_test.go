package deepseek

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// spawnWaitTimeout bounds the polling helpers below. The deepseek
// stub is a short shell script and finishes almost immediately, but
// a loaded CI machine can spend several hundred milliseconds between
// cmd.Start() returning and the child writing its argv to disk.
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
			t.Fatalf(
				"timeout waiting for %d argv entries at %s, got %d: %v",
				want, callsPath, len(argv), argv,
			)
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
			t.Fatalf(
				"timeout waiting for %q in %s; last contents %q (err=%v)",
				want, logPath, data, err,
			)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// installStub writes a `deepseek-tui` shell script into t.TempDir(),
// prepends that dir to PATH, and returns the path of the file the
// script records its argv into. Args are NUL-separated so multi-line
// prompts round-trip cleanly.
func installStub(
	t *testing.T, stdout string, exitCode int,
) (callsPath string) {
	t.Helper()
	dir := t.TempDir()
	callsPath = filepath.Join(dir, "calls.log")
	stdoutPath := filepath.Join(dir, "stdout.txt")
	if err := os.WriteFile(
		stdoutPath, []byte(stdout), 0o644,
	); err != nil {
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
	t.Setenv(
		"PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	return callsPath
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

// TestCheckLogin_LoggedIn pins the happy path: doctor --json returns
// a payload with config_present=true and a non-empty api_key.source
// and CheckLogin returns nil.
func TestCheckLogin_LoggedIn(t *testing.T) {
	calls := installStub(t,
		`{"api_key":{"source":"keychain"},"config_present":true}`, 0)
	if err := New().CheckLogin(t.Context()); err != nil {
		t.Fatalf("CheckLogin: %v", err)
	}
	if argv := readCalls(t, calls); !reflect.DeepEqual(
		argv, []string{"doctor", "--json"},
	) {
		t.Fatalf("argv = %v", argv)
	}
}

// TestCheckLogin_LoggedOut covers the config_present=false branch.
func TestCheckLogin_LoggedOut(t *testing.T) {
	installStub(t,
		`{"api_key":{"source":"keychain"},"config_present":false}`, 0)
	err := New().CheckLogin(t.Context())
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
}

// TestCheckLogin_EmptyAPIKey covers the api_key.source=="" branch.
func TestCheckLogin_EmptyAPIKey(t *testing.T) {
	installStub(t,
		`{"api_key":{"source":""},"config_present":true}`, 0)
	err := New().CheckLogin(t.Context())
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
}

// TestCheckLogin_BadJSON treats unparseable output as logged-out.
func TestCheckLogin_BadJSON(t *testing.T) {
	installStub(t, "not json at all", 0)
	err := New().CheckLogin(t.Context())
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
}

// TestCheckLogin_RunnerError covers the non-zero exit branch.
func TestCheckLogin_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	err := New().CheckLogin(t.Context())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "deepseek-tui doctor failed") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "deepseek-tui login") {
		t.Fatalf("err missing remediation hint: %v", err)
	}
}

// stagePlanFiles writes a marker requirements source so DefaultWorkspace
// can derive a real workspace path from req.FromFilePath.
func stagePlanFiles(t *testing.T) (dir, target string) {
	t.Helper()
	dir = t.TempDir()
	target = filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# spec"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, target
}

func planRequest(
	dir, target string, interactive bool, resumeID, logPath string,
) codingagents.PlanRequest {
	return codingagents.PlanRequest{
		FromFilePath:           target,
		Model:                  "deepseek-v4-pro",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            interactive,
		ResumeChatID:           resumeID,
		AgentLogPath:           logPath,
	}
}

// TestPlan_Interactive pins the interactive flow's argv shape: just
// the top args (`-w <ws>`). deepseek-tui's interactive TUI is driven
// by the user — there is no prompt-as-positional contract — so the
// prompt body never reaches argv. The headless `exec` subcommand
// flags (`exec`, `--model`, `--auto`) and the resume `-r` must NOT
// appear when ResumeChatID is empty.
func TestPlan_Interactive(t *testing.T) {
	dir, target := stagePlanFiles(t)
	calls := installStub(t, "", 0)

	pid, err := New().Plan(t.Context(), planRequest(dir, target, true, "", ""))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid != 0 {
		t.Fatalf("Plan pid = %d, want 0 for interactive", pid)
	}
	argv := readCalls(t, calls)
	want := []string{"-w", dir}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for _, banned := range []string{"exec", "--model", "--auto", "-r"} {
		for _, a := range argv {
			if a == banned {
				t.Fatalf(
					"interactive Plan should not pass %q: argv = %v",
					banned, argv,
				)
			}
		}
	}
}

// TestPlan_Interactive_Resume pins the interactive resume flow's argv:
// top args grow `-r <id>` and the prompt body remains the trailing
// positional.
func TestPlan_Interactive_Resume(t *testing.T) {
	dir, target := stagePlanFiles(t)
	calls := installStub(t, "", 0)
	rid := "11111111-1111-4111-8111-111111111111"

	_, err := New().Plan(
		t.Context(), planRequest(dir, target, true, rid, ""),
	)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	argv := readCalls(t, calls)
	want := []string{"-w", dir, "-r", rid}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
}

// TestPlan_Headless pins the fire-and-forget argv shape and confirms
// the stub's stdout reaches AgentLogPath via SpawnIn.
func TestPlan_Headless(t *testing.T) {
	dir, target := stagePlanFiles(t)
	logPath := filepath.Join(dir, "agent.log")
	calls := installStub(t, "ok\n", 0)

	pid, err := New().Plan(
		t.Context(), planRequest(dir, target, false, "", logPath),
	)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid <= 0 {
		t.Fatalf(
			"Plan pid = %d, want > 0 for headless background spawn", pid,
		)
	}
	want := []string{
		"-w", dir, "exec", "--model", "deepseek-v4-pro", "--auto", "--",
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
	waitForLog(t, logPath, "ok")
}

// TestPlan_Headless_Resume covers the headless resume argv shape.
func TestPlan_Headless_Resume(t *testing.T) {
	dir, target := stagePlanFiles(t)
	logPath := filepath.Join(dir, "agent.log")
	calls := installStub(t, "ok\n", 0)
	rid := "22222222-2222-4222-8222-222222222222"

	_, err := New().Plan(
		t.Context(), planRequest(dir, target, false, rid, logPath),
	)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{
		"-w", dir, "-r", rid,
		"exec", "--model", "deepseek-v4-pro", "--auto", "--",
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

// TestPlan_Interactive_RunnerError pins the wrapped-error shape on a
// non-zero exit during an interactive Plan.
func TestPlan_Interactive_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	dir, target := stagePlanFiles(t)
	pid, err := New().Plan(
		t.Context(), planRequest(dir, target, true, "", ""),
	)
	if err == nil || !strings.Contains(err.Error(), "deepseek-tui") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0 on error", pid)
	}
}

// TestPlan_Headless_SpawnError exercises the SpawnIn-failure branch
// with the directory-as-log trick.
func TestPlan_Headless_SpawnError(t *testing.T) {
	installStub(t, "", 0)
	dir, target := stagePlanFiles(t)
	logPath := filepath.Join(dir, "agent.log")
	if err := os.MkdirAll(logPath, 0o755); err != nil {
		t.Fatal(err)
	}
	pid, err := New().Plan(
		t.Context(), planRequest(dir, target, false, "", logPath),
	)
	if err == nil || !strings.Contains(err.Error(), "deepseek-tui") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0 on Spawn error", pid)
	}
}

func workRequest(
	plan string, interactive bool, resumeID, logPath string,
) codingagents.WorkRequest {
	return codingagents.WorkRequest{
		PlanPath:     plan,
		Model:        "deepseek-v4-pro",
		Interactive:  interactive,
		ResumeChatID: resumeID,
		AgentLogPath: logPath,
	}
}

// TestWork covers Work in the four matrix cells (interactive vs
// headless, fresh vs resume) by walking the cases in a single
// table-driven test.
func TestWork(t *testing.T) {
	cases := []struct {
		name        string
		interactive bool
		resume      string
	}{
		{"interactive-fresh", true, ""},
		{
			"interactive-resume", true,
			"33333333-3333-4333-8333-333333333333",
		},
		{"headless-fresh", false, ""},
		{
			"headless-resume", false,
			"44444444-4444-4444-8444-444444444444",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			plan := filepath.Join(dir, "plan.md")
			if err := os.WriteFile(
				plan, []byte("1. step"), 0o600,
			); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(dir, "agent.log")
			calls := installStub(t, "ok\n", 0)

			pid, err := New().Work(
				t.Context(),
				workRequest(plan, tc.interactive, tc.resume, logPath),
			)
			if err != nil {
				t.Fatalf("Work: %v", err)
			}
			if tc.interactive {
				if pid != 0 {
					t.Fatalf("interactive pid = %d, want 0", pid)
				}
			} else {
				if pid <= 0 {
					t.Fatalf("headless pid = %d, want > 0", pid)
				}
			}

			want := []string{"-w", dir}
			if tc.resume != "" {
				want = append(want, "-r", tc.resume)
			}
			if !tc.interactive {
				want = append(want, "exec",
					"--model", "deepseek-v4-pro", "--auto", "--")
			}
			expectedLen := len(want)
			if !tc.interactive {
				expectedLen++ // trailing prompt positional
			}
			var argv []string
			if tc.interactive {
				argv = readCalls(t, calls)
			} else {
				argv = waitForCalls(t, calls, expectedLen)
			}
			if len(argv) != expectedLen {
				t.Fatalf("argv = %v, want len %d", argv, expectedLen)
			}
			for i, v := range want {
				if argv[i] != v {
					t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
				}
			}
		})
	}
}

func verifyRequest(
	dir string, interactive bool, resumeID, logPath string,
) codingagents.VerifyRequest {
	return codingagents.VerifyRequest{
		RequirementsPath:           filepath.Join(dir, "requirements.md"),
		PlanPath:                   filepath.Join(dir, "plan.md"),
		VerifierPlanOutputPath:     filepath.Join(dir, "verifier_plan.md"),
		VerifierFindingsOutputPath: filepath.Join(dir, "verifier_findings.md"),
		Model:                      "deepseek-v4-pro",
		Interactive:                interactive,
		ResumeChatID:               resumeID,
		AgentLogPath:               logPath,
	}
}

// TestVerify covers Verify in the four matrix cells. cmd.Dir for the
// verifier is the project root (ProjectRootWorkspace); the test
// chdirs into a fresh tempdir so the asserted workspace path is
// predictable.
func TestVerify(t *testing.T) {
	cases := []struct {
		name        string
		interactive bool
		resume      string
	}{
		{"interactive-fresh", true, ""},
		{
			"interactive-resume", true,
			"55555555-5555-4555-8555-555555555555",
		},
		{"headless-fresh", false, ""},
		{
			"headless-resume", false,
			"66666666-6666-4666-8666-666666666666",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("Getwd: %v", err)
			}
			logPath := filepath.Join(dir, "agent.log")
			calls := installStub(t, "ok\n", 0)

			pid, err := New().Verify(
				t.Context(),
				verifyRequest(dir, tc.interactive, tc.resume, logPath),
			)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if tc.interactive {
				if pid != 0 {
					t.Fatalf("interactive pid = %d, want 0", pid)
				}
			} else {
				if pid <= 0 {
					t.Fatalf("headless pid = %d, want > 0", pid)
				}
			}

			want := []string{"-w", cwd}
			if tc.resume != "" {
				want = append(want, "-r", tc.resume)
			}
			if !tc.interactive {
				want = append(want, "exec",
					"--model", "deepseek-v4-pro", "--auto", "--")
			}
			expectedLen := len(want)
			if !tc.interactive {
				expectedLen++ // trailing prompt positional
			}
			var argv []string
			if tc.interactive {
				argv = readCalls(t, calls)
			} else {
				argv = waitForCalls(t, calls, expectedLen)
			}
			if len(argv) != expectedLen {
				t.Fatalf("argv = %v, want len %d", argv, expectedLen)
			}
			for i, v := range want {
				if argv[i] != v {
					t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
				}
			}
		})
	}
}

// TestWork_RunnerError pins the interactive-error branch's wrapped
// error shape and zero pid. The headless path's spawn-error branch
// is exercised by TestPlan_Headless_SpawnError; Work and Verify
// share the same helper so duplicating the assertion here would be
// redundant.
func TestWork_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	dir := t.TempDir()
	plan := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(plan, []byte("1. step"), 0o600); err != nil {
		t.Fatal(err)
	}
	pid, err := New().Work(t.Context(), workRequest(plan, true, "", ""))
	if err == nil || !strings.Contains(err.Error(), "deepseek-tui") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0 on error", pid)
	}
}

// TestVerify_RunnerError pins the verify interactive-error branch.
func TestVerify_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	dir := t.TempDir()
	t.Chdir(dir)
	pid, err := New().Verify(
		t.Context(), verifyRequest(dir, true, "", ""),
	)
	if err == nil || !strings.Contains(err.Error(), "deepseek-tui") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0 on error", pid)
	}
}
