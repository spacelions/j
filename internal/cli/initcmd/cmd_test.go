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

	"github.com/spacelions/j/internal/store"
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
	err := Run(context.Background(), Options{
		Stdout: &stdout,
		Stderr: &stderr,
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	dirExists(t, filepath.Join(dir, ".j"))
	dirExists(t, filepath.Join(dir, ".j", store.TasksDirName))
	fileExists(t, filepath.Join(dir, ".j", "settings"))
	fileExists(t, filepath.Join(dir, ".j", store.TasksDirName, store.TasksDBName))
	if !strings.Contains(stdout.String(), "initialized ") {
		t.Fatalf("stdout = %q, want initialized", stdout.String())
	}
}

func TestRun_FreshInit_DoesNotPrompt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	ui := &scriptedUI{}
	if err := Run(context.Background(), Options{
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
	if err := Run(context.Background(), Options{
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
	if err := Run(context.Background(), Options{
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
	if err := Run(context.Background(), Options{
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
	if err := Run(context.Background(), Options{
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
	fileExists(t, filepath.Join(dir, ".j", store.TasksDirName, store.TasksDBName))
}

func TestRun_UIError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("ui boom")
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{err: boom},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
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
	cmd.SetContext(context.Background())
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
}

// TestNew_FlagEnv covers the env-var binding so INIT_YES=true flips
// init.yes from CI without a flag.
func TestNew_FlagEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("INIT_YES", "true")
	_ = New()
	if !viper.GetBool("init.yes") {
		t.Error("INIT_YES=true should make init.yes true")
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
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
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

// readMustread is the legacy alias retained so the --mustread test
// bodies stay terse.
func readMustread(t *testing.T) (string, bool) {
	t.Helper()
	return readProjectKey(t, "mustread")
}

// TestRun_FreshInit_SeedsMaxIterations pins the unconditional seed:
// every successful `j init` writes project.max_iterations=3, and the
// user can override it later via `j settings set`.
func TestRun_FreshInit_SeedsMaxIterations(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, set := readProjectKey(t, "max_iterations")
	if !set || got != "3" {
		t.Fatalf("project.max_iterations = (%q, %v), want (\"3\", true)", got, set)
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
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
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
	if err := Run(context.Background(), Options{
		Yes:    true,
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, set := readProjectKey(t, "max_iterations")
	if !set || got != "3" {
		t.Fatalf("project.max_iterations after reset = (%q, %v), want (\"3\", true)", got, set)
	}
}

// TestRun_MustreadFlag_SeedsValue pins the new --mustread flag: when
// Options.Mustread is non-nil, Run persists the pointed-to string
// verbatim under project.mustread so the next preflight-gated command
// short-circuits the prompt.
func TestRun_MustreadFlag_SeedsValue(t *testing.T) {
	t.Chdir(t.TempDir())
	v := "AGENTS.md;CLAUDE.md"
	if err := Run(context.Background(), Options{
		Yes:      true,
		Mustread: &v,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		UI:       &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, set := readMustread(t)
	if !set {
		t.Fatal("project.mustread should be persisted")
	}
	if got != v {
		t.Fatalf("project.mustread = %q, want %q (case-preserved)", got, v)
	}
}

// TestRun_MustreadFlag_BlankIsPersisted pins the empty-string branch:
// `--mustread=""` seeds the empty string verbatim, mirroring the
// "blank input is valid" preflight contract.
func TestRun_MustreadFlag_BlankIsPersisted(t *testing.T) {
	t.Chdir(t.TempDir())
	empty := ""
	if err := Run(context.Background(), Options{
		Yes:      true,
		Mustread: &empty,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		UI:       &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, set := readMustread(t)
	if !set || got != "" {
		t.Fatalf("readMustread = (%q, %v), want (\"\", true)", got, set)
	}
}

// TestRun_MustreadFlag_AbsentLeavesUnset confirms Options.Mustread==nil
// does NOT seed the key: the next preflight-gated command will still
// surface the must-read prompt.
func TestRun_MustreadFlag_AbsentLeavesUnset(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Run(context.Background(), Options{
		Yes:    true,
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, set := readMustread(t); set {
		t.Fatal("project.mustread should be unset when --mustread is not passed")
	}
}

// TestNew_MustreadFlagWiring exercises the cobra wiring: passing
// --mustread on the command line populates Options.Mustread via
// cmd.Flags().Changed, and the persisted value matches the flag.
func TestNew_MustreadFlagWiring(t *testing.T) {
	t.Chdir(t.TempDir())
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	cmd.SetArgs([]string{"--yes", "--mustread=AGENTS.md;CLAUDE.md"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, set := readMustread(t)
	if !set || got != "AGENTS.md;CLAUDE.md" {
		t.Fatalf("readMustread = (%q, %v), want (\"AGENTS.md;CLAUDE.md\", true)", got, set)
	}
}
