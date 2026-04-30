package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	bolt "go.etcd.io/bbolt"

	"github.com/spacelions/j/internal/store"
)

// fakeUI is the scripted UI fake used by the orchestration tests.
// confirmReturn / confirmErr program the ConfirmDelete response and
// calls counts every invocation so tests can assert the prompt
// fires (or is bypassed by --yes). pickReturn / pickErr program the
// PickTask response and pickCalls / lastPickedFrom let tests
// inspect what the picker would have been driven over. The
// lastTaskID field still pins the confirmed row so the existing
// happy-path assertions stay terse.
type fakeUI struct {
	confirmReturn  bool
	confirmErr     error
	calls          int
	lastTaskID     string
	pickReturn     string
	pickErr        error
	pickCalls      int
	lastPickedFrom []store.Task
}

func (u *fakeUI) ConfirmDelete(_ context.Context, task store.Task) (bool, error) {
	u.calls++
	u.lastTaskID = task.ID
	if u.confirmErr != nil {
		return false, u.confirmErr
	}
	return u.confirmReturn, nil
}

func (u *fakeUI) PickTask(_ context.Context, tasks []store.Task) (string, error) {
	u.pickCalls++
	u.lastPickedFrom = tasks
	if u.pickErr != nil {
		return "", u.pickErr
	}
	return u.pickReturn, nil
}

// seedTask writes a Task row into the freshly-initialised tasks DB
// and creates the matching <cwd>/.j/tasks/<id>/ subdirectory plus a
// requirements.md sentinel inside it. Returns the absolute task dir
// so callers can stat it after the delete and the underlying store
// (closed before the run, reopened by callers when they need to
// assert post-state).
func seedTask(t *testing.T, id, summary string) string {
	t.Helper()
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.PutTask(store.Task{
		ID:           id,
		Status:       store.StatusPlanDone,
		InvokedTool:  "cursor",
		InvokedModel: "sonnet-4",
		Summary:      summary,
	}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	taskDir, err := store.EnsureTaskDir(id)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, store.RequirementsFileName),
		[]byte("# "+summary+"\nbody"), 0o644); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	return taskDir
}

// taskExists reopens the bbolt DB and reports whether GetTask
// returns the row (true), reports fs.ErrNotExist (false), or fails
// (test failure). Callers use this after RunDelete to assert the
// row was either removed or left intact.
func taskExists(t *testing.T, id string) bool {
	t.Helper()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	_, err = s.GetTask(id)
	if err == nil {
		return true
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	t.Fatalf("GetTask: %v", err)
	return false
}

func TestRunDelete_HappyPath_ConfirmedRemovesRowAndDir(t *testing.T) {
	t.Chdir(t.TempDir())
	taskDir := seedTask(t, "id-happy", "do the thing")
	ui := &fakeUI{confirmReturn: true}
	var stdout, stderr bytes.Buffer
	err := RunDelete(context.Background(), DeleteOptions{
		TaskID: "id-happy",
		Stdout: &stdout,
		Stderr: &stderr,
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunDelete: %v", err)
	}
	if ui.calls != 1 {
		t.Fatalf("UI calls = %d, want 1", ui.calls)
	}
	if ui.lastTaskID != "id-happy" {
		t.Fatalf("UI last task = %q, want id-happy", ui.lastTaskID)
	}
	if taskExists(t, "id-happy") {
		t.Fatal("task row should be gone after delete")
	}
	if _, err := os.Stat(taskDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("task dir should be gone, stat err = %v", err)
	}
	if !strings.Contains(stdout.String(), "J: deleted id-happy") {
		t.Fatalf("stdout = %q, want J: deleted id-happy", stdout.String())
	}
}

func TestRunDelete_YesFlag_SkipsPrompt(t *testing.T) {
	t.Chdir(t.TempDir())
	taskDir := seedTask(t, "id-yes", "yes-flag bypass")
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunDelete(context.Background(), DeleteOptions{
		TaskID: "id-yes",
		Yes:    true,
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunDelete: %v", err)
	}
	if ui.calls != 0 {
		t.Fatalf("UI calls = %d, want 0 with --yes", ui.calls)
	}
	if taskExists(t, "id-yes") {
		t.Fatal("task row should be gone with --yes")
	}
	if _, err := os.Stat(taskDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("task dir should be gone with --yes")
	}
	if !strings.Contains(stdout.String(), "J: deleted id-yes") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunDelete_Decline_LeavesRowAndDirIntact(t *testing.T) {
	t.Chdir(t.TempDir())
	taskDir := seedTask(t, "id-keep", "keep me")
	ui := &fakeUI{confirmReturn: false}
	var stdout bytes.Buffer
	err := RunDelete(context.Background(), DeleteOptions{
		TaskID: "id-keep",
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunDelete: %v", err)
	}
	if ui.calls != 1 {
		t.Fatalf("UI calls = %d, want 1", ui.calls)
	}
	if !taskExists(t, "id-keep") {
		t.Fatal("task row should remain after decline")
	}
	if info, err := os.Stat(taskDir); err != nil || !info.IsDir() {
		t.Fatalf("task dir should remain after decline: err=%v", err)
	}
	if !strings.Contains(stdout.String(), abortedMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), abortedMessage)
	}
}

func TestRunDelete_MissingTask_PrintsNoTask(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunDelete(context.Background(), DeleteOptions{
		TaskID: "ghost",
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunDelete: %v", err)
	}
	if ui.calls != 0 {
		t.Fatalf("UI calls = %d, want 0 on missing task", ui.calls)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != noTaskMessage {
		t.Fatalf("stdout = %q, want exactly %q", got, noTaskMessage)
	}
}

// TestRunDelete_UIErrorPropagates pins the explicit-error branch:
// when the UI returns a non-nil error (something other than
// huh.ErrUserAborted, which the implementation collapses into
// (false, nil)), RunDelete must propagate it wrapped to the caller.
func TestRunDelete_UIErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTask(t, "id-ui-err", "boom")
	boom := errors.New("ui boom")
	err := RunDelete(context.Background(), DeleteOptions{
		TaskID: "id-ui-err",
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{confirmErr: boom},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	// Row and dir must remain because the prompt errored before the
	// confirm branch ran.
	if !taskExists(t, "id-ui-err") {
		t.Fatal("task row should remain after UI error")
	}
}

// TestRunDelete_GetTaskNonNotExistError exercises the propagate
// branch in RunDelete: a non-NotExist GetTask error (here, a JSON
// decode error from a corrupted bucket value) must surface to the
// caller and the store must be closed before the return.
func TestRunDelete_GetTaskNonNotExistError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(store.BucketTasks))
		if err != nil {
			return err
		}
		return b.Put([]byte("bad"), []byte("not-json"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	err = RunDelete(context.Background(), DeleteOptions{
		TaskID: "bad",
		Yes:    true,
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "decode task") {
		t.Fatalf("err = %v, want decode-task error", err)
	}
}

// TestRunDelete_OpenError points the tasks DB path at an existing
// directory so bolt.Open fails, exercising the open-error branch.
func TestRunDelete_OpenError(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	err = RunDelete(context.Background(), DeleteOptions{
		TaskID: "x",
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{},
	})
	if err == nil {
		t.Fatal("expected open error when tasks path is a directory")
	}
}

// TestRunDelete_DefaultTasksPathError replaces cwd with one that we
// remove so DefaultTasksDBPath -> os.Getwd fails. On macOS / FUSE
// getwd may succeed via cached inodes; in that case the test skips.
func TestRunDelete_DefaultTasksPathError(t *testing.T) {
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
	if err := RunDelete(context.Background(), DeleteOptions{
		TaskID: "x",
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{},
	}); err == nil {
		t.Fatal("expected DefaultTasksDBPath to surface getwd error")
	}
}

// TestRunDelete_RemoveTaskDirError exercises the on-disk teardown
// failure branch: the per-task dir exists, the bbolt row is
// successfully removed, but RemoveTaskDir cannot unlink the
// directory because its parent (.j/tasks) is read-only. Skipped on
// root and Windows.
func TestRunDelete_RemoveTaskDirError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	seedTask(t, "id-rm-fail", "won't remove")
	tasksDir := filepath.Join(dir, ".j", store.TasksDirName)
	if err := os.Chmod(tasksDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tasksDir, 0o755) })
	err := RunDelete(context.Background(), DeleteOptions{
		TaskID: "id-rm-fail",
		Yes:    true,
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{},
	})
	if err == nil {
		t.Fatal("expected RemoveTaskDir to fail under read-only parent")
	}
	if !strings.Contains(err.Error(), "tasks delete") {
		t.Fatalf("err = %v, want wrapped 'tasks delete' error", err)
	}
}

// TestNewDeleteCmd_Smoke pins the command shape: registered name
// and flags. The --id flag is no longer MarkFlagRequired (the
// picker fallback covers the empty-id case) so the test asserts
// the absence of the required annotation explicitly.
func TestNewDeleteCmd_Smoke(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newDeleteCmd()
	if cmd == nil {
		t.Fatal("newDeleteCmd returned nil")
	}
	if cmd.Use != "delete" {
		t.Fatalf("Use = %q, want delete", cmd.Use)
	}
	idFlag := cmd.Flags().Lookup("id")
	if idFlag == nil {
		t.Fatal("--id flag was not registered")
	}
	if v, ok := idFlag.Annotations[cobra.BashCompOneRequiredFlag]; ok && len(v) > 0 && v[0] == "true" {
		t.Fatalf("--id should not be MarkFlagRequired; annotations=%v", idFlag.Annotations)
	}
	if cmd.Flags().Lookup("yes") == nil {
		t.Fatal("--yes flag was not registered")
	}
	if cmd.RunE == nil {
		t.Fatal("RunE is nil")
	}
}

// TestNewDeleteCmd_FlagDefaults pins the registered defaults and
// viper bindings. The --id flag is now optional (no MarkFlagRequired)
// so the test asserts both the empty default and the absence of the
// required annotation.
func TestNewDeleteCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newDeleteCmd()
	idFlag := cmd.Flags().Lookup("id")
	if idFlag == nil || idFlag.DefValue != "" {
		t.Fatalf("--id default = %q, want empty", idFlag.DefValue)
	}
	if v, ok := idFlag.Annotations[cobra.BashCompOneRequiredFlag]; ok && len(v) > 0 && v[0] == "true" {
		t.Fatalf("--id should not be marked required; annotations=%v", idFlag.Annotations)
	}
	yesFlag := cmd.Flags().Lookup("yes")
	if yesFlag == nil || yesFlag.DefValue != "false" {
		t.Fatalf("--yes default = %q, want false", yesFlag.DefValue)
	}
	if viper.GetBool("tasks.delete.yes") {
		t.Error("tasks.delete.yes should default to false via BindPFlag")
	}
}

// TestNewDeleteCmd_FlagEnv pins the env-var bindings: TASKS_DELETE_ID
// and TASKS_DELETE_YES feed viper without an explicit flag.
func TestNewDeleteCmd_FlagEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_DELETE_ID", "from-env")
	t.Setenv("TASKS_DELETE_YES", "true")
	_ = newDeleteCmd()
	if got := viper.GetString("tasks.delete.id"); got != "from-env" {
		t.Errorf("tasks.delete.id = %q, want from-env", got)
	}
	if !viper.GetBool("tasks.delete.yes") {
		t.Error("TASKS_DELETE_YES=true should make tasks.delete.yes true")
	}
}

// TestNewDeleteCmd_RunE_ExecutesEnvYes drives the cobra command's
// RunE with --id supplied as a flag and TASKS_DELETE_YES forcing
// the prompt-skip path through the env binding. cobra's
// MarkFlagRequired only inspects pflag's Changed state so --id
// must come from argv (env-only wouldn't satisfy the required
// guard); this test still exercises the env-fed --yes branch end
// to end alongside the flag plumbing.
func TestNewDeleteCmd_RunE_ExecutesEnvYes(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	taskDir := seedTask(t, "id-env", "via env")
	t.Setenv("TASKS_DELETE_YES", "true")
	root := New()
	var stdout bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetContext(context.Background())
	root.SetArgs([]string{"delete", "--id", "id-env"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout.String(), "J: deleted id-env") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if taskExists(t, "id-env") {
		t.Fatal("task row should be gone after delete via env wiring")
	}
	if _, err := os.Stat(taskDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("task dir should be gone, stat err = %v", err)
	}
}

// TestDeleteOptions_WithDefaults_FillsAllNilStreams exercises the
// nil-default branches of the helper without invoking RunDelete.
func TestDeleteOptions_WithDefaults_FillsAllNilStreams(t *testing.T) {
	o := DeleteOptions{}.withDefaults()
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

// TestDeleteOptions_WithDefaults_KeepsProvidedUI pins the non-nil
// branch in withDefaults: a caller-supplied UI is preserved instead
// of being clobbered by newHuhUI.
func TestDeleteOptions_WithDefaults_KeepsProvidedUI(t *testing.T) {
	custom := &fakeUI{}
	o := DeleteOptions{UI: custom, Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}.withDefaults()
	if o.UI != custom {
		t.Errorf("UI = %v, want custom fake", o.UI)
	}
}

// TestRunDelete_PickerHappyPath drives the picker fallback end to
// end: with no --id, the scripted PickTask returns an existing id
// and ConfirmDelete approves; the row + dir must be gone and stdout
// must carry the deleted-id marker.
func TestRunDelete_PickerHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	taskDir := seedTask(t, "id-pick", "pick me")
	ui := &fakeUI{pickReturn: "id-pick", confirmReturn: true}
	var stdout bytes.Buffer
	err := RunDelete(context.Background(), DeleteOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunDelete: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if len(ui.lastPickedFrom) != 1 || ui.lastPickedFrom[0].ID != "id-pick" {
		t.Fatalf("PickTask received tasks = %v, want [id-pick]", ui.lastPickedFrom)
	}
	if ui.calls != 1 || ui.lastTaskID != "id-pick" {
		t.Fatalf("ConfirmDelete calls = %d, lastTaskID = %q", ui.calls, ui.lastTaskID)
	}
	if taskExists(t, "id-pick") {
		t.Fatal("task row should be gone after picker delete")
	}
	if _, err := os.Stat(taskDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("task dir should be gone, stat err = %v", err)
	}
	if !strings.Contains(stdout.String(), "J: deleted id-pick") {
		t.Fatalf("stdout = %q, want J: deleted id-pick", stdout.String())
	}
}

// TestRunDelete_PickerAbort exercises the cancel signal: PickTask
// returns ("", nil); RunDelete must short-circuit before touching
// ConfirmDelete or DeleteTask. Row and dir stay intact.
func TestRunDelete_PickerAbort(t *testing.T) {
	t.Chdir(t.TempDir())
	taskDir := seedTask(t, "id-abort", "keep me")
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunDelete(context.Background(), DeleteOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunDelete: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if ui.calls != 0 {
		t.Fatalf("ConfirmDelete calls = %d, want 0 on picker abort", ui.calls)
	}
	if !taskExists(t, "id-abort") {
		t.Fatal("task row should remain after picker abort")
	}
	if info, err := os.Stat(taskDir); err != nil || !info.IsDir() {
		t.Fatalf("task dir should remain after picker abort: err=%v", err)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty on picker abort", stdout.String())
	}
}

// TestRunDelete_PickerErrorPropagates pins the error branch: a
// non-aborted PickTask error must surface to the caller and leave
// the row + dir intact.
func TestRunDelete_PickerErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())
	taskDir := seedTask(t, "id-pick-err", "boom")
	boom := errors.New("picker boom")
	ui := &fakeUI{pickErr: boom}
	err := RunDelete(context.Background(), DeleteOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     ui,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if ui.calls != 0 {
		t.Fatalf("ConfirmDelete calls = %d, want 0 on picker error", ui.calls)
	}
	if !taskExists(t, "id-pick-err") {
		t.Fatal("task row should remain after picker error")
	}
	if info, err := os.Stat(taskDir); err != nil || !info.IsDir() {
		t.Fatalf("task dir should remain after picker error: err=%v", err)
	}
}

// TestRunDelete_NoIDMissingDB exercises the "no list.db, no --id"
// short-circuit: emptyMessage on stdout, no UI invocation, return
// nil.
func TestRunDelete_NoIDMissingDB(t *testing.T) {
	t.Chdir(t.TempDir())
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunDelete(context.Background(), DeleteOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunDelete: %v", err)
	}
	if ui.pickCalls != 0 || ui.calls != 0 {
		t.Fatalf("UI calls = pick=%d, confirm=%d, want both 0", ui.pickCalls, ui.calls)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != emptyMessage {
		t.Fatalf("stdout = %q, want %q", got, emptyMessage)
	}
}

// TestRunDelete_NoIDEmptyBucket exercises the empty-bucket branch:
// list.db exists but holds no rows. No picker fires; emptyMessage
// is printed and RunDelete returns nil.
func TestRunDelete_NoIDEmptyBucket(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunDelete(context.Background(), DeleteOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunDelete: %v", err)
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask calls = %d, want 0 on empty bucket", ui.pickCalls)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != emptyMessage {
		t.Fatalf("stdout = %q, want %q", got, emptyMessage)
	}
}

// TestRunDelete_NoIDListDecodeError plants a non-JSON value into
// the tasks bucket so ListTasks fails after the picker branch
// opens the store. The error must propagate; no UI is invoked.
func TestRunDelete_NoIDListDecodeError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(store.BucketTasks))
		if err != nil {
			return err
		}
		return b.Put([]byte("bad"), []byte("not-json"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ui := &fakeUI{}
	err = RunDelete(context.Background(), DeleteOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     ui,
	})
	if err == nil {
		t.Fatal("expected ListTasks decode error to propagate")
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask calls = %d, want 0 on list decode error", ui.pickCalls)
	}
}

