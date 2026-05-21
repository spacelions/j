package initcmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// scriptedUI returns a pre-set boolean from ConfirmReset and tracks
// invocation count so tests can assert prompts fire (or don't).
type scriptedUI struct {
	confirm bool
	err     error
	calls   int
}

func (u *scriptedUI) ConfirmReset(context.Context) (bool, error) {
	u.calls++
	if u.err != nil {
		return false, u.err
	}
	return u.confirm, nil
}

// fileExists is a small helper so each filesystem assertion stays a
// single line in the test bodies.
func fileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %q to exist: %v", path, err)
	}
}

func dirExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %q to exist: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", path)
	}
}

func TestRun_FreshInitCreatesAllArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	err := Run(t.Context(), Options{
		Stdout: &stdout,
		Stderr: &stderr,
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	dirExists(t, filepath.Join(dir, ".j"))
	dirExists(t, filepath.Join(dir, ".j", tasks.DirName))
	fileExists(t, filepath.Join(dir, ".j", "settings"))
	if !strings.Contains(stdout.String(), "initialized ") {
		t.Fatalf("stdout = %q, want initialized", stdout.String())
	}
}

func TestRun_FreshInit_DoesNotPrompt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	ui := &scriptedUI{}
	if err := Run(t.Context(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     ui,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.calls != 0 {
		t.Fatalf("UI.ConfirmReset calls = %d, want 0", ui.calls)
	}
}

func TestRun_AlreadyInitialized_PromptAccept_WipesAndRecreates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	// Drop a sentinel file inside .j to confirm the wipe ran.
	sentinel := filepath.Join(dir, ".j", "marker.txt")
	if err := os.WriteFile(sentinel, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ui := &scriptedUI{confirm: true}
	if err := Run(t.Context(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     ui,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.calls != 1 {
		t.Fatalf("UI calls = %d, want 1", ui.calls)
	}
	if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sentinel should be gone after wipe: %v", err)
	}
	fileExists(t, filepath.Join(dir, ".j", "settings"))
}

func TestRun_AlreadyInitialized_PromptDecline_LeavesUntouched(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, ".j", "marker.txt")
	if err := os.WriteFile(sentinel, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ui := &scriptedUI{confirm: false}
	var stdout bytes.Buffer
	if err := Run(t.Context(), Options{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout.String(), "init aborted") {
		t.Fatalf("stdout = %q, want init aborted", stdout.String())
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel should still exist after decline: %v", err)
	}
}

func TestRun_AlreadyInitialized_YesFlag_SkipsPrompt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, ".j", "marker.txt")
	if err := os.WriteFile(sentinel, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ui := &scriptedUI{}
	if err := Run(t.Context(), Options{
		Yes:    true,
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     ui,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.calls != 0 {
		t.Fatalf("UI calls = %d, want 0 with --yes", ui.calls)
	}
	if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sentinel should be gone: %v", err)
	}
}

func TestRun_PartialState_FillsMissingArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".j"), 0o755); err != nil {
		t.Fatal(err)
	}
	ui := &scriptedUI{}
	if err := Run(t.Context(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     ui,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.calls != 0 {
		t.Fatalf("UI calls = %d, want 0 (partial state should not prompt)", ui.calls)
	}
	fileExists(t, filepath.Join(dir, ".j", "settings"))
}

func TestRun_UIError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("ui boom")
	err := Run(t.Context(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{err: boom},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

// TestSeedDefaults_OpenError pins the store.Open error branch in
// seedDefaults: making the settings path a directory causes bolt.Open
// to fail, so the error propagates.
func TestSeedDefaults_OpenError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Replace the settings file with a directory.
	path := store.DefaultPath()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := seedDefaults(nil, nil); err == nil {
		t.Fatal("expected error when settings is a directory")
	}
}

func TestRun_NewSmoke(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := New()
	if cmd == nil {
		t.Fatal("New returned nil")
	}
	if cmd.Use != "init" {
		t.Fatalf("Use = %q, want init", cmd.Use)
	}
	if f := cmd.Flags().Lookup("yes"); f == nil {
		t.Fatal("--yes flag was not registered")
	}
	if cmd.RunE == nil {
		t.Fatal("RunE is nil")
	}
}

// TestNew_RunE_ExecutesInTempDir drives the cobra command's RunE in a
// temp dir so the rest of Run is exercised through the cobra wiring.
func TestNew_RunE_ExecutesInTempDir(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	dir := t.TempDir()
	t.Chdir(dir)
	cmd := New()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetContext(t.Context())
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	dirExists(t, filepath.Join(dir, ".j"))
}

// TestNew_FlagDefaults pins the registered flag default and the
// viper key it binds to.
func TestNew_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := New()
	f := cmd.Flags().Lookup("yes")
	if f == nil {
		t.Fatal("yes flag not registered")
	}
	if f.DefValue != "false" {
		t.Fatalf("--yes default = %q, want false", f.DefValue)
	}
	if viper.GetBool("init.yes") {
		t.Error("init.yes should default to false via BindPFlag")
	}
	if f := cmd.Flags().Lookup("must-read"); f == nil {
		t.Fatal("--must-read flag was not registered")
	}
	approval := cmd.Flags().Lookup("plan-requires-approval")
	if approval == nil {
		t.Fatal("--plan-requires-approval flag was not registered")
	}
	if approval.DefValue != "true" {
		t.Fatalf("--plan-requires-approval default = %q, want true", approval.DefValue)
	}
}

// TestNew_FlagEnv covers the env-var binding so INIT_YES=true flips
// init.yes from CI without a flag.
func TestNew_FlagEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("INIT_YES", "true")
	t.Setenv("INIT_PLAN_REQUIRES_APPROVAL", "false")
	_ = New()
	if !viper.GetBool("init.yes") {
		t.Error("INIT_YES=true should make init.yes true")
	}
	if viper.GetBool("init.plan_requires_approval") {
		t.Error("INIT_PLAN_REQUIRES_APPROVAL=false should make init.plan_requires_approval false")
	}
}

// TestWithDefaults_FillsAllNilStreams exercises the Stdin/Stdout/Stderr
// nil-default branches in withDefaults. We assert each field becomes
// the matching os.Std* handle without invoking Run (which would write
// to those handles during the test).
func TestWithDefaults_FillsAllNilStreams(t *testing.T) {
	o := Options{}.withDefaults()
	if o.Stdin != os.Stdin {
		t.Errorf("Stdin = %v, want os.Stdin", o.Stdin)
	}
	if o.Stdout != os.Stdout {
		t.Errorf("Stdout = %v, want os.Stdout", o.Stdout)
	}
	if o.Stderr != os.Stderr {
		t.Errorf("Stderr = %v, want os.Stderr", o.Stderr)
	}
	if o.UI == nil {
		t.Error("UI was not defaulted")
	}
}

// readProjectKey reads a key from the project bucket of the current
// cwd's settings store and returns (value, set).
func readProjectKey(t *testing.T, key string) (string, bool) {
	t.Helper()
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	v, set, err := s.Get(store.BucketProject, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return v, set
}

// readMustRead keeps the --must-read test bodies terse.
func readMustRead(t *testing.T) (string, bool) {
	t.Helper()
	return readProjectKey(t, resolver.KeyMustRead)
}

// TestRun_FreshInit_SeedsMaxIterations pins the unconditional seed:
// every successful `j init` writes project.max_iterations=3, and the
// user can override it later via `j settings set`.
func TestRun_FreshInit_SeedsMaxIterations(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Run(t.Context(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, set := readProjectKey(t, store.KeyMaxIterations)
	if !set || got != "3" {
		t.Fatalf("project.max_iterations = (%q, %v), want (\"3\", true)", got, set)
	}
}

// TestRun_FreshInit_SeedsPlanRequiresApproval pins the default gate:
// fresh projects pause after planning unless the user opts out.
func TestRun_FreshInit_SeedsPlanRequiresApproval(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Run(t.Context(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, set := readProjectKey(t, store.KeyPlanRequiresApproval)
	if !set || got != "true" {
		t.Fatalf("project.plan_requires_approval = (%q, %v), want (\"true\", true)", got, set)
	}
}

func TestRun_PlanRequiresApprovalFalse_SeedsFalse(t *testing.T) {
	t.Chdir(t.TempDir())
	v := false
	if err := Run(t.Context(), Options{
		PlanRequiresApproval: &v,
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		UI:                   &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, set := readProjectKey(t, store.KeyPlanRequiresApproval)
	if !set || got != "false" {
		t.Fatalf("project.plan_requires_approval = (%q, %v), want (\"false\", true)", got, set)
	}
}

// TestRun_ResetReseedsMaxIterations confirms the reset-and-recreate
// path also reseeds project.max_iterations: a stale value persisted
// before the reset is overwritten with the fresh default of "3".
func TestRun_ResetReseedsMaxIterations(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketProject, "max_iterations", "99"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), Options{
		Yes:    true,
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, set := readProjectKey(t, store.KeyMaxIterations)
	if !set || got != "3" {
		t.Fatalf("project.max_iterations after reset = (%q, %v), want (\"3\", true)", got, set)
	}
}

// TestRun_MustReadFlag_SeedsValue pins the new --must-read flag: when
// Options.MustRead is non-nil, Run persists the pointed-to string
// verbatim under project.must_read so the next preflight-gated command
// short-circuits the prompt.
func TestRun_MustReadFlag_SeedsValue(t *testing.T) {
	t.Chdir(t.TempDir())
	v := "AGENTS.md;CLAUDE.md"
	if err := Run(t.Context(), Options{
		Yes:      true,
		MustRead: &v,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		UI:       &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, set := readMustRead(t)
	if !set {
		t.Fatal("project.must_read should be persisted")
	}
	if got != v {
		t.Fatalf("project.must_read = %q, want %q (case-preserved)", got, v)
	}
}

// TestRun_MustReadFlag_BlankIsPersisted pins the empty-string branch:
// `--must-read=""` seeds the empty string verbatim, mirroring the
// "blank input is valid" preflight contract.
func TestRun_MustReadFlag_BlankIsPersisted(t *testing.T) {
	t.Chdir(t.TempDir())
	empty := ""
	if err := Run(t.Context(), Options{
		Yes:      true,
		MustRead: &empty,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		UI:       &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, set := readMustRead(t)
	if !set || got != "" {
		t.Fatalf("readMustRead = (%q, %v), want (\"\", true)", got, set)
	}
}

// TestRun_MustReadFlag_AbsentLeavesUnset confirms Options.MustRead==nil
// does NOT seed the key: the next preflight-gated command will still
// surface the must-read prompt.
func TestRun_MustReadFlag_AbsentLeavesUnset(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Run(t.Context(), Options{
		Yes:    true,
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, set := readMustRead(t); set {
		t.Fatal("project.must_read should be unset when --must-read is not passed")
	}
}

// TestNew_MustReadFlagWiring exercises the cobra wiring: passing
// --must-read on the command line populates Options.MustRead via
// cmd.Flags().Changed, and the persisted value matches the flag.
func TestNew_MustReadFlagWiring(t *testing.T) {
	t.Chdir(t.TempDir())
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	cmd.SetArgs([]string{"--yes", "--must-read=AGENTS.md;CLAUDE.md"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, set := readMustRead(t)
	if !set || got != "AGENTS.md;CLAUDE.md" {
		t.Fatalf("readMustRead = (%q, %v), want (\"AGENTS.md;CLAUDE.md\", true)", got, set)
	}
}

func TestNew_PlanRequiresApprovalFlagWiring(t *testing.T) {
	t.Chdir(t.TempDir())
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	cmd.SetArgs([]string{"--yes", "--plan-requires-approval=false"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, set := readProjectKey(t, store.KeyPlanRequiresApproval)
	if !set || got != "false" {
		t.Fatalf("project.plan_requires_approval = (%q, %v), want (\"false\", true)", got, set)
	}
}

// TestRun_ProjectInitializedError covers the ProjectInitialized error branch:
// a symlink loop at the .j path makes os.Stat return ELOOP (not ENOENT).
func TestRun_ProjectInitializedError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jDir := store.DefaultDir()
	if err := os.Symlink(jDir, jDir); err != nil {
		t.Fatal(err)
	}
	err := Run(t.Context(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected error for symlink-loop .j path")
	}
}

// TestRun_EnsureProjectError covers the EnsureProject error branch:
// a read-only parent directory prevents MkdirAll from creating .j/.
func TestRun_EnsureProjectError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	t.Chdir(dir)
	err := Run(t.Context(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected error for read-only parent dir")
	}
}
