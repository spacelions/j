package deepseek

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/testutil"
)

// spawnWaitTimeout bounds the polling helpers below. The deepseek
// stub is a short shell script and finishes almost immediately, but
// a loaded CI machine can spend several hundred milliseconds between
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

// installStub writes a `deepseek-tui` shell script into t.TempDir(),
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

func useFakeDefaultHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(envHome, "")
	t.Setenv("HOME", root)
	return filepath.Join(root, ".deepseek")
}

func writeLegacySession(t *testing.T, root, name, id string) string {
	t.Helper()
	path := filepath.Join(root, "sessions", name+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"metadata": map[string]any{
			"id":         id,
			"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertSymlinkTarget(t *testing.T, link, want string) {
	t.Helper()
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink %s: %v", link, err)
	}
	if got != want {
		t.Fatalf("readlink %s = %q, want %q", link, got, want)
	}
}

func TestDefaultHomeEnvOverride(t *testing.T) {
	t.Setenv(envHome, "/tmp/deepseek-home")
	if got := defaultHome(); got != "/tmp/deepseek-home" {
		t.Fatalf("defaultHome = %q", got)
	}
}

func TestPopulateScopedHomeShadowsDefaultHome(t *testing.T) {
	realHome := useFakeDefaultHome(t)
	if err := os.MkdirAll(filepath.Join(realHome, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(realHome, "auth.json")
	configPath := filepath.Join(realHome, "config.toml")
	if err := os.WriteFile(authPath, []byte(`{"token":"t"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		configPath, []byte("model = 'x'\n"), 0o600,
	); err != nil {
		t.Fatal(err)
	}

	taskDir := t.TempDir()
	got, err := populateScopedHome(taskDir)
	if err != nil {
		t.Fatalf("populateScopedHome: %v", err)
	}
	want := filepath.Join(taskDir, homeSubdir)
	if got != want {
		t.Fatalf("scoped home = %q, want %q", got, want)
	}
	assertSymlinkTarget(t, filepath.Join(got, "auth.json"), authPath)
	assertSymlinkTarget(t, filepath.Join(got, "config.toml"), configPath)
	if info, err := os.Lstat(filepath.Join(got, "sessions")); err != nil {
		t.Fatalf("stat sessions: %v", err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("scoped sessions must be a private directory")
	}
	if _, err := populateScopedHome(taskDir); err != nil {
		t.Fatalf("populateScopedHome second call: %v", err)
	}
}

func TestPopulateScopedHomeMissingDefaultHome(t *testing.T) {
	useFakeDefaultHome(t)
	taskDir := t.TempDir()
	got, err := populateScopedHome(taskDir)
	if err != nil {
		t.Fatalf("populateScopedHome: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got, "sessions")); err != nil {
		t.Fatalf("stat sessions: %v", err)
	}
}

func TestPopulateScopedHomeReadDirError(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envHome, homeFile)

	_, err := populateScopedHome(t.TempDir())
	if err == nil {
		t.Fatal("populateScopedHome error = nil")
	}
}

func TestPopulateScopedHomeMkdirErrors(t *testing.T) {
	taskFile := filepath.Join(t.TempDir(), "task-file")
	if err := os.WriteFile(taskFile, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := populateScopedHome(taskFile); err == nil {
		t.Fatal("populateScopedHome home mkdir error = nil")
	}

	taskDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(taskDir, homeSubdir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionsDir(taskDir), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := populateScopedHome(taskDir); err == nil {
		t.Fatal("populateScopedHome sessions mkdir error = nil")
	}
}

func TestPopulateScopedHomeSymlinkError(t *testing.T) {
	realHome := useFakeDefaultHome(t)
	if err := os.MkdirAll(realHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(realHome, "auth.json"), []byte("{}"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	taskDir := t.TempDir()
	scoped := filepath.Join(taskDir, homeSubdir)
	if err := os.MkdirAll(sessionsDir(taskDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scoped, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(scoped, 0o700) })

	if _, err := populateScopedHome(taskDir); err == nil {
		t.Fatal("populateScopedHome symlink error = nil")
	}
}

func TestMigrateLegacySession(t *testing.T) {
	realHome := useFakeDefaultHome(t)
	src := writeLegacySession(t, realHome, "match", "legacy-id")
	writeLegacySession(t, realHome, "other", "other-id")
	taskDir := t.TempDir()
	if _, err := populateScopedHome(taskDir); err != nil {
		t.Fatalf("populateScopedHome: %v", err)
	}

	if err := migrateLegacySession(taskDir, "legacy-id"); err != nil {
		t.Fatalf("migrateLegacySession: %v", err)
	}
	target := filepath.Join(sessionsDir(taskDir), "match.json")
	assertSymlinkTarget(t, target, src)
	if err := migrateLegacySession(taskDir, "legacy-id"); err != nil {
		t.Fatalf("migrateLegacySession second call: %v", err)
	}
}

func TestMigrateLegacySessionNoops(t *testing.T) {
	if err := migrateLegacySession(t.TempDir(), ""); err != nil {
		t.Fatalf("migrateLegacySession empty: %v", err)
	}
	realHome := useFakeDefaultHome(t)
	if err := migrateLegacySession(t.TempDir(), "missing"); err != nil {
		t.Fatalf("migrateLegacySession missing store: %v", err)
	}
	corrupt := filepath.Join(realHome, "sessions", "corrupt.json")
	if err := os.MkdirAll(filepath.Dir(corrupt), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corrupt, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeLegacySession(t, realHome, "other", "other-id")
	if err := migrateLegacySession(t.TempDir(), "absent"); err != nil {
		t.Fatalf("migrateLegacySession absent: %v", err)
	}
}

func TestMigrateLegacySessionReadDirError(t *testing.T) {
	realHome := useFakeDefaultHome(t)
	if err := os.MkdirAll(realHome, 0o700); err != nil {
		t.Fatal(err)
	}
	err := os.Symlink("sessions", filepath.Join(realHome, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	err = migrateLegacySession(t.TempDir(), "resume-id")
	if err == nil {
		t.Fatal("migrateLegacySession error = nil")
	}
}

func TestPrepareScopedEnvErrors(t *testing.T) {
	taskPath := filepath.Join(t.TempDir(), "task-file")
	if err := os.WriteFile(taskPath, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareScopedEnv(taskPath, ""); err == nil {
		t.Fatal("prepareScopedEnv populate error = nil")
	}

	realHome := useFakeDefaultHome(t)
	if err := os.MkdirAll(realHome, 0o700); err != nil {
		t.Fatal(err)
	}
	err := os.Symlink("sessions", filepath.Join(realHome, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareScopedEnv(t.TempDir(), "resume-id"); err == nil {
		t.Fatal("prepareScopedEnv migrate error = nil")
	}
}

func TestPrepareScopedEnvMigratesLegacySession(t *testing.T) {
	realHome := useFakeDefaultHome(t)
	src := writeLegacySession(t, realHome, "match", "resume-id")
	taskDir := t.TempDir()

	env, err := prepareScopedEnv(taskDir, "resume-id")
	if err != nil {
		t.Fatalf("prepareScopedEnv: %v", err)
	}
	wantHome := filepath.Join(taskDir, homeSubdir)
	if !reflect.DeepEqual(env, []string{envHome + "=" + wantHome}) {
		t.Fatalf("env = %v, want scoped home", env)
	}
	target := filepath.Join(sessionsDir(taskDir), "match.json")
	assertSymlinkTarget(t, target, src)
}

func TestSymlinkIfMissingError(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "missing", "dst")
	if err := symlinkIfMissing(src, dst); err == nil {
		t.Fatal("symlinkIfMissing error = nil")
	}
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
		TaskDir:                dir,
		FromFilePath:           target,
		Model:                  "deepseek-v4-pro",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            interactive,
		ResumeChatID:           resumeID,
		AgentLogPath:           logPath,
	}
}

func TestPlan_SetsScopedHomeEnv(t *testing.T) {
	dir, target := stagePlanFiles(t)
	stub := testutil.InstallExecutableStub(
		t,
		testutil.ExecutableStubOptions{
			Binary:    Binary,
			ExitCode:  0,
			RecordEnv: true,
		},
	)

	if _, err := New().Plan(
		t.Context(), planRequest(dir, target, true, "", ""),
	); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	env := testutil.ReadTrimmedFile(t, stub.EnvPath)
	want := envHome + "=" + filepath.Join(dir, homeSubdir)
	if !strings.Contains(env, want) {
		t.Fatalf("env missing %q: %s", want, env)
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

func TestPlan_Work_Verify_ScopedHomeError(t *testing.T) {
	dir, target := stagePlanFiles(t)
	taskPath := filepath.Join(t.TempDir(), "task-as-file")
	if err := os.WriteFile(taskPath, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("1. step"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := New()
	cases := []struct {
		name string
		run  func() (int, error)
	}{
		{
			name: "plan",
			run: func() (int, error) {
				req := planRequest(dir, target, true, "", "")
				req.TaskDir = taskPath
				return a.Plan(t.Context(), req)
			},
		},
		{
			name: "work",
			run: func() (int, error) {
				req := workRequest(planPath, true, "", "")
				req.TaskDir = taskPath
				return a.Work(t.Context(), req)
			},
		},
		{
			name: "verify",
			run: func() (int, error) {
				req := verifyRequest(dir, true, "", "")
				req.TaskDir = taskPath
				return a.Verify(t.Context(), req)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pid, err := tc.run()
			if err == nil ||
				!strings.Contains(err.Error(), "deepseek-tui") {
				t.Fatalf(
					"err = %v, want deepseek scoped-home error",
					err,
				)
			}
			if pid != 0 {
				t.Fatalf("pid = %d, want 0", pid)
			}
		})
	}
}

func workRequest(
	plan string, interactive bool, resumeID, logPath string,
) codingagents.WorkRequest {
	return codingagents.WorkRequest{
		TaskDir:      filepath.Dir(plan),
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
		TaskDir:                    dir,
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
