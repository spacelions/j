package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// fakeSpawner is the scripted Spawner used by RunEnter tests. It
// records every invocation and returns a programmable error so the
// subshell branch is asserted without launching a real shell.
type fakeSpawner struct {
	mu       sync.Mutex
	calls    int
	lastDir  string
	lastIn   io.Reader
	lastOut  io.Writer
	lastErr  io.Writer
	returnErr error
}

func (f *fakeSpawner) Spawn(_ context.Context, dir string, in io.Reader, out, errw io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastDir = dir
	f.lastIn = in
	f.lastOut = out
	f.lastErr = errw
	return f.returnErr
}

// withFakeSpawner returns a Spawner closure that funnels into the
// supplied fake. Tests pass it through EnterOptions.Spawner so the
// concrete Spawner func type is exercised directly.
func withFakeSpawner(f *fakeSpawner) Spawner { return f.Spawn }

// TestNew_HasEnterSubcommand pins the registration of the enter
// child on the parent `tasks` constructor. Detailed flag/runtime
// behaviour lives in the rest of this file.
func TestNew_HasEnterSubcommand(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == "enter" {
			return
		}
	}
	t.Fatal("expected `enter` subcommand to be registered on `j tasks`")
}

// TestNewEnterCmd_Smoke pins the command shape: registered name and
// flags. Neither --id nor --print is MarkFlagRequired so the test
// asserts the absence of the required annotation.
func TestNewEnterCmd_Smoke(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newEnterCmd()
	if cmd == nil {
		t.Fatal("newEnterCmd returned nil")
	}
	if cmd.Use != "enter" {
		t.Fatalf("Use = %q, want enter", cmd.Use)
	}
	for _, name := range []string{"id", "print"} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Fatalf("--%s flag was not registered", name)
		}
		if v, ok := f.Annotations["cobra_annotation_bash_completion_one_required_flag"]; ok && len(v) > 0 && v[0] == "true" {
			t.Fatalf("--%s should not be MarkFlagRequired; annotations=%v", name, f.Annotations)
		}
	}
	if cmd.RunE == nil {
		t.Fatal("RunE is nil")
	}
}

// TestNewEnterCmd_FlagDefaults pins the registered defaults and
// viper bindings for the enter subcommand.
func TestNewEnterCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newEnterCmd()
	idFlag := cmd.Flags().Lookup("id")
	if idFlag == nil || idFlag.DefValue != "" {
		t.Fatalf("--id default = %q, want empty", idFlag.DefValue)
	}
	printFlag := cmd.Flags().Lookup("print")
	if printFlag == nil || printFlag.DefValue != "false" {
		t.Fatalf("--print default = %q, want false", printFlag.DefValue)
	}
	if viper.GetString("tasks.enter.id") != "" {
		t.Error("tasks.enter.id should default to empty via BindPFlag")
	}
	if viper.GetBool("tasks.enter.print") {
		t.Error("tasks.enter.print should default to false via BindPFlag")
	}
}

// TestNewEnterCmd_FlagEnv pins the env-var bindings for both flags.
func TestNewEnterCmd_FlagEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_ENTER_ID", "env-id")
	t.Setenv("TASKS_ENTER_PRINT", "true")
	_ = newEnterCmd()
	if got := viper.GetString("tasks.enter.id"); got != "env-id" {
		t.Errorf("tasks.enter.id = %q, want env-id", got)
	}
	if !viper.GetBool("tasks.enter.print") {
		t.Error("TASKS_ENTER_PRINT=true should make tasks.enter.print true")
	}
}

// TestRunEnter_PickerSubshell exercises the picker happy path: with
// no --id, the scripted PickTask returns the seeded id and the fake
// Spawner records the dir. Stdout stays empty; the resolved dir
// must be <cwd>/.j/tasks/<id>.
func TestRunEnter_PickerSubshell(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTask(t, "id-pick", "first")
	seedTask(t, "id-pick-2", "second")
	ui := &fakeUI{pickReturn: "id-pick"}
	spawner := &fakeSpawner{}
	var stdout, stderr bytes.Buffer
	err = RunEnter(context.Background(), EnterOptions{
		Stdout:  &stdout,
		Stderr:  &stderr,
		UI:      ui,
		Spawner: withFakeSpawner(spawner),
	})
	if err != nil {
		t.Fatalf("RunEnter: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if len(ui.lastPickedFrom) != 2 {
		t.Fatalf("PickTask received %d tasks, want 2", len(ui.lastPickedFrom))
	}
	if spawner.calls != 1 {
		t.Fatalf("Spawner calls = %d, want 1", spawner.calls)
	}
	wantDir := filepath.Join(cwd, ".j", tasks.DirName, "id-pick")
	if spawner.lastDir != wantDir {
		t.Fatalf("Spawner dir = %q, want %q", spawner.lastDir, wantDir)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty for subshell branch", stdout.String())
	}
}

// TestRunEnter_PickerPrint exercises the picker + --print branch:
// stdout is exactly <dir>\n and the Spawner is never invoked.
func TestRunEnter_PickerPrint(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTask(t, "id-print", "print me")
	ui := &fakeUI{pickReturn: "id-print"}
	spawner := &fakeSpawner{}
	var stdout bytes.Buffer
	err = RunEnter(context.Background(), EnterOptions{
		Print:   true,
		Stdout:  &stdout,
		Stderr:  io.Discard,
		UI:      ui,
		Spawner: withFakeSpawner(spawner),
	})
	if err != nil {
		t.Fatalf("RunEnter: %v", err)
	}
	if spawner.calls != 0 {
		t.Fatalf("Spawner calls = %d, want 0 on --print", spawner.calls)
	}
	wantDir := filepath.Join(cwd, ".j", tasks.DirName, "id-print")
	got := strings.TrimRight(stdout.String(), "\n")
	if got != wantDir {
		t.Fatalf("stdout = %q, want %q", got, wantDir)
	}
}

// TestRunEnter_DirectIDSubshell exercises the --id direct path
// (subshell): UI is never called; the resolved dir is the seeded
// task dir.
func TestRunEnter_DirectIDSubshell(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTask(t, "id-direct", "direct")
	ui := &fakeUI{}
	spawner := &fakeSpawner{}
	var stdout bytes.Buffer
	err = RunEnter(context.Background(), EnterOptions{
		TaskID:  "id-direct",
		Stdout:  &stdout,
		Stderr:  io.Discard,
		UI:      ui,
		Spawner: withFakeSpawner(spawner),
	})
	if err != nil {
		t.Fatalf("RunEnter: %v", err)
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask calls = %d, want 0 on --id branch", ui.pickCalls)
	}
	if spawner.calls != 1 {
		t.Fatalf("Spawner calls = %d, want 1", spawner.calls)
	}
	wantDir := filepath.Join(cwd, ".j", tasks.DirName, "id-direct")
	if spawner.lastDir != wantDir {
		t.Fatalf("Spawner dir = %q, want %q", spawner.lastDir, wantDir)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty for subshell", stdout.String())
	}
}

// TestRunEnter_DirectIDPrint exercises --id with --print: stdout is
// exactly <dir>\n; UI / Spawner are both untouched.
func TestRunEnter_DirectIDPrint(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTask(t, "id-direct-print", "x")
	ui := &fakeUI{}
	spawner := &fakeSpawner{}
	var stdout bytes.Buffer
	err = RunEnter(context.Background(), EnterOptions{
		TaskID:  "id-direct-print",
		Print:   true,
		Stdout:  &stdout,
		Stderr:  io.Discard,
		UI:      ui,
		Spawner: withFakeSpawner(spawner),
	})
	if err != nil {
		t.Fatalf("RunEnter: %v", err)
	}
	if ui.pickCalls != 0 || spawner.calls != 0 {
		t.Fatalf("UI/Spawner calls = pick=%d, spawn=%d, want both 0", ui.pickCalls, spawner.calls)
	}
	wantDir := filepath.Join(cwd, ".j", tasks.DirName, "id-direct-print")
	got := strings.TrimRight(stdout.String(), "\n")
	if got != wantDir {
		t.Fatalf("stdout = %q, want %q", got, wantDir)
	}
}

// TestRunEnter_DirectIDUnknown exercises the missing-task short
// circuit: stdout is `J: no task` exactly, exit 0, and no UI /
// Spawner is invoked.
func TestRunEnter_DirectIDUnknown(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	ui := &fakeUI{}
	spawner := &fakeSpawner{}
	var stdout bytes.Buffer
	err := RunEnter(context.Background(), EnterOptions{
		TaskID:  "ghost",
		Stdout:  &stdout,
		Stderr:  io.Discard,
		UI:      ui,
		Spawner: withFakeSpawner(spawner),
	})
	if err != nil {
		t.Fatalf("RunEnter: %v", err)
	}
	if ui.pickCalls != 0 || spawner.calls != 0 {
		t.Fatalf("UI/Spawner calls = pick=%d, spawn=%d, want both 0 on unknown id", ui.pickCalls, spawner.calls)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != noTaskMessage {
		t.Fatalf("stdout = %q, want %q", got, noTaskMessage)
	}
}

// TestRunEnter_NoIDMissingDB exercises the empty store short
// circuit: with no list.db and no --id, RunEnter prints
// emptyMessage, returns nil, and never touches UI / Spawner.
func TestRunEnter_NoIDMissingDB(t *testing.T) {
	t.Chdir(t.TempDir())
	ui := &fakeUI{}
	spawner := &fakeSpawner{}
	var stdout bytes.Buffer
	err := RunEnter(context.Background(), EnterOptions{
		Stdout:  &stdout,
		Stderr:  io.Discard,
		UI:      ui,
		Spawner: withFakeSpawner(spawner),
	})
	if err != nil {
		t.Fatalf("RunEnter: %v", err)
	}
	if ui.pickCalls != 0 || spawner.calls != 0 {
		t.Fatalf("UI/Spawner calls = pick=%d, spawn=%d, want both 0", ui.pickCalls, spawner.calls)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != emptyMessage {
		t.Fatalf("stdout = %q, want %q", got, emptyMessage)
	}
}

// TestRunEnter_NoIDEmptyBucket exercises the empty-bucket branch:
// list.db exists but holds no rows. emptyMessage prints; no picker
// fires.
func TestRunEnter_NoIDEmptyBucket(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	ui := &fakeUI{}
	spawner := &fakeSpawner{}
	var stdout bytes.Buffer
	err := RunEnter(context.Background(), EnterOptions{
		Stdout:  &stdout,
		Stderr:  io.Discard,
		UI:      ui,
		Spawner: withFakeSpawner(spawner),
	})
	if err != nil {
		t.Fatalf("RunEnter: %v", err)
	}
	if ui.pickCalls != 0 || spawner.calls != 0 {
		t.Fatalf("UI/Spawner calls = pick=%d, spawn=%d, want both 0", ui.pickCalls, spawner.calls)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != emptyMessage {
		t.Fatalf("stdout = %q, want %q", got, emptyMessage)
	}
}

// TestRunEnter_PickerAbort exercises the user-cancel signal: the
// scripted PickTask returns ("", nil); RunEnter must short-circuit
// without invoking the Spawner.
func TestRunEnter_PickerAbort(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTask(t, "id-abort", "abort me")
	ui := &fakeUI{}
	spawner := &fakeSpawner{}
	var stdout bytes.Buffer
	err := RunEnter(context.Background(), EnterOptions{
		Stdout:  &stdout,
		Stderr:  io.Discard,
		UI:      ui,
		Spawner: withFakeSpawner(spawner),
	})
	if err != nil {
		t.Fatalf("RunEnter: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if spawner.calls != 0 {
		t.Fatalf("Spawner calls = %d, want 0 on user abort", spawner.calls)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty on user abort", stdout.String())
	}
}

// TestRunEnter_PickerErrorPropagates pins the error branch: a
// non-aborted PickTask error must surface; Spawner stays untouched.
func TestRunEnter_PickerErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTask(t, "id-pick-err", "boom")
	boom := errors.New("picker boom")
	ui := &fakeUI{pickErr: boom}
	spawner := &fakeSpawner{}
	err := RunEnter(context.Background(), EnterOptions{
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		UI:      ui,
		Spawner: withFakeSpawner(spawner),
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if spawner.calls != 0 {
		t.Fatalf("Spawner calls = %d, want 0 on UI error", spawner.calls)
	}
}

// TestRunEnter_SpawnerErrorPropagates exercises the spawner-error
// branch: the fake Spawner returns an error and RunEnter wraps it
// with the "tasks enter" prefix.
func TestRunEnter_SpawnerErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTask(t, "id-spawn-err", "spawn err")
	boom := errors.New("spawn boom")
	ui := &fakeUI{}
	spawner := &fakeSpawner{returnErr: boom}
	err := RunEnter(context.Background(), EnterOptions{
		TaskID:  "id-spawn-err",
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		UI:      ui,
		Spawner: withFakeSpawner(spawner),
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v wrapped", err, boom)
	}
	if !strings.Contains(err.Error(), "tasks enter") {
		t.Fatalf("err = %v, want wrapped 'tasks enter' prefix", err)
	}
}

// TestRunEnter_GetTaskNonNotExistError exercises the propagate
// branch on the --id direct path: a non-NotExist GetTask error
// (here, a JSON decode error from a corrupted bucket value) must
// surface.
func TestRunEnter_GetTaskNonNotExistError(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	testutil.SeedRawTaskFile(t, "bad", []byte("not = valid = toml"))
	err := RunEnter(context.Background(), EnterOptions{
		TaskID:  "bad",
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		UI:      &fakeUI{},
		Spawner: withFakeSpawner(&fakeSpawner{}),
	})
	if err == nil || !strings.Contains(err.Error(), "decode task") {
		t.Fatalf("err = %v, want decode-task error", err)
	}
}

// TestRunEnter_ListDecodeError_Picker plants a non-JSON value into
// the bucket and exercises the picker branch's ListTasks path.
func TestRunEnter_ListDecodeError_Picker(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	testutil.SeedRawTaskFile(t, "bad", []byte("not = valid = toml"))
	ui := &fakeUI{}
	err := RunEnter(context.Background(), EnterOptions{
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		UI:      ui,
		Spawner: withFakeSpawner(&fakeSpawner{}),
	})
	if err == nil {
		t.Fatal("expected list decode error to propagate")
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask calls = %d, want 0 on list decode error", ui.pickCalls)
	}
}

// TestRunEnter_DefaultTasksPathError mirrors the same-named test in
// delete_test.go: replace cwd with one that is then removed so
// DefaultTasksDir -> os.Getwd fails. Skipped on root / windows /
// macOS-FUSE-cached-inode environments where getwd still succeeds.
func TestRunEnter_DefaultTasksPathError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cwd cannot be removed while in use on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root may bypass relevant FS errors")
	}
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	t.Setenv("PWD", "")
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("os.Getwd unexpectedly succeeded")
	}
	if err := RunEnter(context.Background(), EnterOptions{
		TaskID:  "x",
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		UI:      &fakeUI{},
		Spawner: withFakeSpawner(&fakeSpawner{}),
	}); err == nil {
		t.Fatal("expected DefaultTasksDir to surface getwd error on direct branch")
	}
}

// TestRunEnter_DefaultTasksPathError_Picker exercises the same
// failure mode on the picker branch (no --id).
func TestRunEnter_DefaultTasksPathError_Picker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cwd cannot be removed while in use on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root may bypass relevant FS errors")
	}
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	t.Setenv("PWD", "")
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("os.Getwd unexpectedly succeeded")
	}
	if err := RunEnter(context.Background(), EnterOptions{
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		UI:      &fakeUI{},
		Spawner: withFakeSpawner(&fakeSpawner{}),
	}); err == nil {
		t.Fatal("expected DefaultTasksDir to surface getwd error on picker branch")
	}
}

// TestEnterOptions_WithDefaults_FillsAllNilStreams exercises the
// nil-default branches without invoking RunEnter.
func TestEnterOptions_WithDefaults_FillsAllNilStreams(t *testing.T) {
	o := EnterOptions{}.withDefaults()
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
	if o.Spawner == nil {
		t.Error("Spawner was not defaulted")
	}
}

// TestEnterOptions_WithDefaults_KeepsProvided pins the non-nil
// branches: caller-supplied UI / Spawner are preserved.
func TestEnterOptions_WithDefaults_KeepsProvided(t *testing.T) {
	customUI := &fakeUI{}
	called := false
	customSpawner := func(context.Context, string, io.Reader, io.Writer, io.Writer) error {
		called = true
		return nil
	}
	o := EnterOptions{
		UI:      customUI,
		Spawner: customSpawner,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	}.withDefaults()
	if o.UI != customUI {
		t.Errorf("UI = %v, want custom fake", o.UI)
	}
	if err := o.Spawner(context.Background(), "/", nil, nil, nil); err != nil {
		t.Fatalf("custom spawner err = %v", err)
	}
	if !called {
		t.Error("custom Spawner was not invoked")
	}
}

// TestDefaultSpawner_RunsCommand exercises the production Spawner
// against a small POSIX command. /bin/sh is universally available
// on the supported platforms; the test feeds an `echo` stdin so
// the shell prints a sentinel and exits cleanly. Touching cmd.Dir
// is verified separately in TestDefaultSpawner_RunsInDir.
func TestDefaultSpawner_RunsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("default spawner targets POSIX shells")
	}
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	var stdout bytes.Buffer
	err := defaultSpawner(context.Background(), dir, strings.NewReader("echo spawner-ran\n"), &stdout, io.Discard)
	if err != nil {
		t.Fatalf("defaultSpawner: %v", err)
	}
	if !strings.Contains(stdout.String(), "spawner-ran") {
		t.Fatalf("stdout = %q, want sentinel 'spawner-ran'", stdout.String())
	}
}

// TestDefaultSpawner_RunsInDir exercises the cmd.Dir wiring: the
// spawned shell creates a sentinel file via redirect and the test
// verifies it landed in the supplied directory. EvalSymlinks
// resolves macOS's /var -> /private/var prefix so the assertion is
// portable across linux + darwin.
func TestDefaultSpawner_RunsInDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("default spawner targets POSIX shells")
	}
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	err := defaultSpawner(context.Background(), dir, strings.NewReader("echo ok > sentinel\n"), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("defaultSpawner: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(resolved, "sentinel")); err != nil {
		t.Fatalf("sentinel file missing in %q: %v", resolved, err)
	}
}

// TestDefaultSpawner_FallbackShell exercises the $SHELL-empty
// branch: with SHELL unset, defaultSpawner must fall back to
// /bin/sh and still complete cleanly.
func TestDefaultSpawner_FallbackShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("default spawner targets POSIX shells")
	}
	t.Setenv("SHELL", "")
	dir := t.TempDir()
	err := defaultSpawner(context.Background(), dir, strings.NewReader("exit 0\n"), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("defaultSpawner fallback: %v", err)
	}
}

// TestNewEnterCmd_RunE_PickerPrintViaEnv exercises the cobra wiring
// of the print path with TASKS_ENTER_ID + --print: the env-bound
// id reaches RunEnter and the absolute task dir is printed to
// stdout. cobra's RunE is the entry point so the env binding is
// proven end to end.
func TestNewEnterCmd_RunE_PickerPrintViaEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTask(t, "id-env-print", "via env")
	t.Setenv("TASKS_ENTER_ID", "id-env-print")
	root := New()
	var stdout bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetContext(context.Background())
	root.SetArgs([]string{"enter", "--print"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantDir := filepath.Join(cwd, ".j", tasks.DirName, "id-env-print")
	got := strings.TrimRight(stdout.String(), "\n")
	if got != wantDir {
		t.Fatalf("stdout = %q, want %q", got, wantDir)
	}
}

// TestNewEnterCmd_RunE_PrintViaEnvFlagID exercises the inverse
// wiring: TASKS_ENTER_PRINT=true with --id supplied as an argv
// flag. The print path must engage end to end without flag-
// suppling --print.
func TestNewEnterCmd_RunE_PrintViaEnvFlagID(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTask(t, "id-env-bool", "via env bool")
	t.Setenv("TASKS_ENTER_PRINT", "true")
	root := New()
	var stdout bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetContext(context.Background())
	root.SetArgs([]string{"enter", "--id", "id-env-bool"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantDir := filepath.Join(cwd, ".j", tasks.DirName, "id-env-bool")
	got := strings.TrimRight(stdout.String(), "\n")
	if got != wantDir {
		t.Fatalf("stdout = %q, want %q", got, wantDir)
	}
}
