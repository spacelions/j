package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// fakeViewer is the scripted Viewer used by the read-leaf tests.
// It records every invocation and returns a programmable error so
// the renderer branch is asserted without executing real bat/cat.
type fakeViewer struct {
	calls     int
	lastPath  string
	lastIn    io.Reader
	lastOut   io.Writer
	lastErr   io.Writer
	returnErr error
}

func (f *fakeViewer) View(
	_ context.Context,
	path string,
	in io.Reader,
	out, errw io.Writer,
) error {
	f.calls++
	f.lastPath = path
	f.lastIn = in
	f.lastOut = out
	f.lastErr = errw
	return f.returnErr
}

func withFakeViewer(f *fakeViewer) Viewer { return f.View }

// withLookPath shadows the package lookPath var for the lifetime of
// the test and restores the production hook on cleanup.
func withLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = orig })
}

// seedTaskWithFile runs testutil.Init, persists a task row, and
// writes the supplied filename inside <cwd>/.j/tasks/<id>/. Used by
// the read / logs / task-view tests so the Viewer is invoked with
// the real on-disk path.
func seedTaskWithFile(
	t *testing.T,
	id, summary, filename, body string,
) string {
	t.Helper()
	testutil.Init(t)
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:        id,
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   summary,
	}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if filename == "" {
		return taskDir
	}
	path := filepath.Join(taskDir, filename)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	return taskDir
}

func TestChooseViewerBinary_BatPreferredOnTTY(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		switch name {
		case "bat":
			return "/usr/local/bin/bat", nil
		case "cat":
			return "/bin/cat", nil
		}
		return "", errors.New("unexpected lookup")
	})
	if got := chooseViewerBinary(true); got != "bat" {
		t.Fatalf("chooseViewerBinary(true) = %q, want bat", got)
	}
}

func TestChooseViewerBinary_CatWhenNotTTY(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		switch name {
		case "bat":
			return "/usr/local/bin/bat", nil
		case "cat":
			return "/bin/cat", nil
		}
		return "", errors.New("unexpected lookup")
	})
	if got := chooseViewerBinary(false); got != "cat" {
		t.Fatalf("chooseViewerBinary(false) = %q, want cat", got)
	}
}

func TestChooseViewerBinary_CatWhenBatMissing(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		if name == "cat" {
			return "/bin/cat", nil
		}
		return "", errors.New("not found")
	})
	if got := chooseViewerBinary(true); got != "cat" {
		t.Fatalf("chooseViewerBinary(true) = %q, want cat", got)
	}
}

func TestChooseViewerBinary_EmptyWhenAllMissing(t *testing.T) {
	withLookPath(t, func(string) (string, error) {
		return "", errors.New("not found")
	})
	if got := chooseViewerBinary(true); got != "" {
		t.Fatalf("chooseViewerBinary = %q, want empty", got)
	}
}

func TestCopyFileTo_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := copyFileTo(path, &buf); err != nil {
		t.Fatalf("copyFileTo: %v", err)
	}
	if buf.String() != "hello" {
		t.Fatalf("buf = %q, want hello", buf.String())
	}
}

func TestCopyFileTo_CopyError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := copyFileTo(path, failingWriter{})
	if err == nil || !strings.Contains(err.Error(), "copy") {
		t.Fatalf("err = %v, want wrapped 'copy' error", err)
	}
}

func TestCopyFileTo_OpenError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.txt")
	err := copyFileTo(missing, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "open") {
		t.Fatalf("err = %v, want open-error", err)
	}
}

func TestDefaultViewer_FallbackToIOCopy(t *testing.T) {
	withLookPath(t, func(string) (string, error) {
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := defaultViewer(
		t.Context(), path, nil, &buf, io.Discard,
	); err != nil {
		t.Fatalf("defaultViewer: %v", err)
	}
	if buf.String() != "body" {
		t.Fatalf("buf = %q, want body", buf.String())
	}
}

func TestDefaultViewer_RunsCatBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX cat")
	}
	withLookPath(t, exec.LookPath)
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("via-cat"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := defaultViewer(
		t.Context(), path, nil, &buf, io.Discard,
	); err != nil {
		t.Fatalf("defaultViewer: %v", err)
	}
	if !strings.Contains(buf.String(), "via-cat") {
		t.Fatalf("buf = %q, want via-cat", buf.String())
	}
}

func TestDefaultViewer_ExecError(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		if name == "cat" {
			return "/path/that/does/not/exist/cat", nil
		}
		return "", errors.New("not found")
	})
	err := defaultViewer(
		t.Context(),
		filepath.Join(t.TempDir(), "x"),
		nil, io.Discard, io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "cat") {
		t.Fatalf("err = %v, want wrapped cat error", err)
	}
}

func TestResolveTaskFile_DirectIDHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTaskWithFile(t, "id-rt", "x",
		tasks.RequirementsFileName, "body")
	got, ok, err := resolveTaskFile(
		t.Context(),
		fileResolveOptions{
			TaskID: "id-rt",
			Stdout: io.Discard,
		},
		tasks.RequirementsFileName,
	)
	if err != nil || !ok {
		t.Fatalf("resolveTaskFile = (%q, %v, %v)", got, ok, err)
	}
	want := filepath.Join(
		cwd, ".j", tasks.DirName, "id-rt", tasks.RequirementsFileName,
	)
	if got != want {
		t.Fatalf("resolved = %q, want %q", got, want)
	}
}

func TestResolveTaskFile_DirectIDUnknown(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	var stdout bytes.Buffer
	got, ok, err := resolveTaskFile(
		t.Context(),
		fileResolveOptions{TaskID: "ghost", Stdout: &stdout},
		tasks.RequirementsFileName,
	)
	if err != nil || ok || got != "" {
		t.Fatalf("expected (\"\", false, nil); got (%q, %v, %v)",
			got, ok, err)
	}
	line := strings.TrimRight(stdout.String(), "\n")
	if line != noTaskMessage {
		t.Fatalf("stdout = %q, want %q", line, noTaskMessage)
	}
}

func TestResolveTaskFile_FileMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-no-file", "x", "", "")
	var stdout bytes.Buffer
	got, ok, err := resolveTaskFile(
		t.Context(),
		fileResolveOptions{TaskID: "id-no-file", Stdout: &stdout},
		tasks.RequirementsFileName,
	)
	if err != nil || ok || got != "" {
		t.Fatalf("expected short-circuit; got (%q, %v, %v)",
			got, ok, err)
	}
	want := "J: " + tasks.RequirementsFileName +
		" not found for task id-no-file"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want substring %q",
			stdout.String(), want)
	}
}

func TestResolveTaskFile_NoIDEmptyStore(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	ui := &fakeUI{}
	var stdout bytes.Buffer
	got, ok, err := resolveTaskFile(
		t.Context(),
		fileResolveOptions{UI: ui, Stdout: &stdout},
		tasks.RequirementsFileName,
	)
	if err != nil || ok || got != "" {
		t.Fatalf("got (%q, %v, %v)", got, ok, err)
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask calls = %d, want 0", ui.pickCalls)
	}
	line := strings.TrimRight(stdout.String(), "\n")
	if line != emptyMessage {
		t.Fatalf("stdout = %q, want %q", line, emptyMessage)
	}
}

func TestResolveTaskFile_NoIDPickerHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTaskWithFile(t, "id-pk", "x",
		tasks.PlanFileName, "plan body")
	ui := &fakeUI{pickReturn: "id-pk"}
	got, ok, err := resolveTaskFile(
		t.Context(),
		fileResolveOptions{UI: ui, Stdout: io.Discard},
		tasks.PlanFileName,
	)
	if err != nil || !ok {
		t.Fatalf("resolveTaskFile = (%q, %v, %v)", got, ok, err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	want := filepath.Join(
		cwd, ".j", tasks.DirName, "id-pk", tasks.PlanFileName,
	)
	if got != want {
		t.Fatalf("resolved = %q, want %q", got, want)
	}
}

func TestResolveTaskFile_NoIDPickerAbort(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-abort", "x",
		tasks.RequirementsFileName, "")
	ui := &fakeUI{}
	var stdout bytes.Buffer
	got, ok, err := resolveTaskFile(
		t.Context(),
		fileResolveOptions{UI: ui, Stdout: &stdout},
		tasks.RequirementsFileName,
	)
	if err != nil || ok || got != "" {
		t.Fatalf("got (%q, %v, %v)", got, ok, err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestResolveTaskFile_GetTaskNonNotExistError(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	testutil.SeedRawTaskFile(t, "bad", []byte("not = valid = toml"))
	_, _, err := resolveTaskFile(
		t.Context(),
		fileResolveOptions{TaskID: "bad", Stdout: io.Discard},
		tasks.RequirementsFileName,
	)
	if err == nil || !strings.Contains(err.Error(), "decode task") {
		t.Fatalf("err = %v, want decode-task error", err)
	}
}

func TestResolveTaskFile_PickerErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-perr", "x",
		tasks.RequirementsFileName, "")
	boom := errors.New("picker boom")
	ui := &fakeUI{pickErr: boom}
	_, _, err := resolveTaskFile(
		t.Context(),
		fileResolveOptions{UI: ui, Stdout: io.Discard},
		tasks.RequirementsFileName,
	)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestResolveTaskFile_StatNonNotExistError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX symlink semantics")
	}
	t.Chdir(t.TempDir())
	taskDir := seedTaskWithFile(t, "id-loop", "x", "", "")
	loopPath := filepath.Join(taskDir, tasks.RequirementsFileName)
	if err := os.Symlink(loopPath, loopPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, ok, err := resolveTaskFile(
		t.Context(),
		fileResolveOptions{TaskID: "id-loop", Stdout: io.Discard},
		tasks.RequirementsFileName,
	)
	if err == nil {
		t.Fatalf("ok=%v, want non-nil error from ELOOP stat", ok)
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Fatalf("err = %v, want wrapped 'stat' prefix", err)
	}
}

func TestResolveTaskFile_DefaultDirError(t *testing.T) {
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
	_, _, err := resolveTaskFile(
		t.Context(),
		fileResolveOptions{TaskID: "x", Stdout: io.Discard},
		tasks.RequirementsFileName,
	)
	if err == nil {
		t.Fatal("expected DefaultDir to surface getwd error")
	}
}
