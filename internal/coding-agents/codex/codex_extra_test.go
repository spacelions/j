package codex

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/testutil"
)

// spawnWaitTimeout bounds the polling helpers below. The codex stub
// is a short shell script and finishes almost immediately, but a
// loaded CI machine can spend several hundred milliseconds between
// cmd.Start() returning and the child writing its argv to disk.
const spawnWaitTimeout = 5 * time.Second

func waitForCalls(t *testing.T, callsPath string, want int) []string {
	t.Helper()
	return testutil.WaitForNullArgs(t, callsPath, want, spawnWaitTimeout)
}

func waitForLog(t *testing.T, logPath, want string) string {
	t.Helper()
	return testutil.WaitForLog(t, logPath, want, spawnWaitTimeout)
}

// installStub writes a `codex` shell script into t.TempDir(),
// prepends that dir to PATH, and returns the path of the file the
// script records its argv into. Args are NUL-separated so multi-line
// prompts round-trip cleanly.
func installStub(
	t *testing.T, stdout string, exitCode int,
) (callsPath string) {
	t.Helper()
	return testutil.InstallExecutableStub(
		t,
		testutil.ExecutableStubOptions{
			Binary:   Binary,
			Stdout:   stdout,
			ExitCode: exitCode,
		},
	).CallsPath
}

func readCalls(t *testing.T, path string) []string {
	t.Helper()
	return testutil.ReadNullArgs(t, path)
}

// TestCheckLogin_LoggedIn pins the happy path: `codex login status`
// exits 0 and CheckLogin returns nil.
func TestCheckLogin_LoggedIn(t *testing.T) {
	calls := installStub(t, "Logged in using ChatGPT\n", 0)
	if err := New().CheckLogin(t.Context()); err != nil {
		t.Fatalf("CheckLogin: %v", err)
	}
	if argv := readCalls(t, calls); !reflect.DeepEqual(
		argv, []string{"login", "status"},
	) {
		t.Fatalf("argv = %v", argv)
	}
}

// TestCheckLogin_LoggedOut covers the non-zero-exit branch.
func TestCheckLogin_LoggedOut(t *testing.T) {
	installStub(t, "Not logged in\n", 1)
	err := New().CheckLogin(t.Context())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "codex login status failed") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "codex login") {
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
		Model:                  "gpt-5.5",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            interactive,
		ResumeChatID:           resumeID,
		AgentLogPath:           logPath,
	}
}

// TestPlan_Interactive pins the interactive flow's argv shape:
// `[-m m] -- <prompt>` with the prompt as the trailing positional.
// The headless `exec` subcommand and resume keyword must NOT appear
// when ResumeChatID is empty.
func TestPlan_Interactive(t *testing.T) {
	dir, target := stagePlanFiles(t)
	calls := installStub(t, "", 0)

	pid, err := New().Plan(
		t.Context(), planRequest(dir, target, true, "", ""),
	)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid != 0 {
		t.Fatalf("Plan pid = %d, want 0 for interactive", pid)
	}
	argv := readCalls(t, calls)
	// Leading args: -m gpt-5.5 -- <prompt>. Length is 4.
	if len(argv) != 4 {
		t.Fatalf("argv = %v, want len 4", argv)
	}
	want := []string{"-m", "gpt-5.5", "--"}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
	for _, banned := range []string{"exec", "resume"} {
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

// TestPlan_Interactive_Resume pins the interactive resume flow's
// argv: `resume <id> -m <m> -- <prompt>`.
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
	if len(argv) != 6 {
		t.Fatalf("argv = %v, want len 6", argv)
	}
	want := []string{"resume", rid, "-m", "gpt-5.5", "--"}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
}

// TestPlan_Headless pins the fire-and-forget argv shape and confirms
// the stub's stdout reaches AgentLogPath via SpawnFormattedIn.
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
		"exec", "-m", "gpt-5.5",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"--",
	}
	argv := waitForCalls(t, calls, len(want)+1)
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v, want len %d", argv, len(want)+1)
	}
	for i, v := range want {
		if argv[i] != v {
			t.Fatalf("arg[%d] = %q, want %q", i, argv[i], v)
		}
	}
	waitForLog(t, logPath, "ok")
}

// TestPlan_Headless_Resume covers the headless resume argv shape:
// `exec resume <id> -m <m> --skip-git-repo-check
// --dangerously-bypass-approvals-and-sandbox -- <prompt>`.
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
		"exec", "resume", rid, "-m", "gpt-5.5",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"--",
	}
	argv := waitForCalls(t, calls, len(want)+1)
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %v, want len %d", argv, len(want)+1)
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
	if err == nil || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0 on error", pid)
	}
}

// TestPlan_Headless_SpawnError exercises the SpawnFormattedIn-failure
// branch with the directory-as-log trick.
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
	if err == nil || !strings.Contains(err.Error(), "codex") {
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
		Model:        "gpt-5.5",
		Interactive:  interactive,
		ResumeChatID: resumeID,
		AgentLogPath: logPath,
	}
}

// TestWork covers Work in the four matrix cells (interactive vs
// headless, fresh vs resume).
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

			want := buildWantArgs(tc.interactive, tc.resume)
			expectedLen := len(want) + 1 // trailing prompt positional
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

// buildWantArgs constructs the expected argv prefix (everything
// before the trailing prompt positional) for the matrix tests.
func buildWantArgs(interactive bool, resume string) []string {
	var want []string
	if !interactive {
		want = append(want, "exec")
	}
	if resume != "" {
		want = append(want, "resume", resume)
	}
	want = append(want, "-m", "gpt-5.5")
	if !interactive {
		want = append(want,
			"--skip-git-repo-check",
			"--dangerously-bypass-approvals-and-sandbox")
	}
	want = append(want, "--")
	return want
}

func verifyRequest(
	dir string, interactive bool, resumeID, logPath string,
) codingagents.VerifyRequest {
	return codingagents.VerifyRequest{
		RequirementsPath:           filepath.Join(dir, "requirements.md"),
		PlanPath:                   filepath.Join(dir, "plan.md"),
		VerifierPlanOutputPath:     filepath.Join(dir, "verifier_plan.md"),
		VerifierFindingsOutputPath: filepath.Join(dir, "verifier_findings.md"),
		Model:                      "gpt-5.5",
		Interactive:                interactive,
		ResumeChatID:               resumeID,
		AgentLogPath:               logPath,
	}
}

// TestVerify covers Verify in the four matrix cells. cmd.Dir for the
// verifier is the project root (ProjectRootWorkspace); the test
// chdirs into a fresh tempdir so the asserted argv is predictable.
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

			want := buildWantArgs(tc.interactive, tc.resume)
			expectedLen := len(want) + 1 // trailing prompt positional
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
// error shape and zero pid.
func TestWork_RunnerError(t *testing.T) {
	installStub(t, "", 1)
	dir := t.TempDir()
	plan := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(plan, []byte("1. step"), 0o600); err != nil {
		t.Fatal(err)
	}
	pid, err := New().Work(t.Context(), workRequest(plan, true, "", ""))
	if err == nil || !strings.Contains(err.Error(), "codex") {
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
	if err == nil || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("err = %v", err)
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0 on error", pid)
	}
}
