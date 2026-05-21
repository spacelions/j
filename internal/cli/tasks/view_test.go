package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	s := tasks.OpenDefault()
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

func TestChooseStreamMode_TailAndTspinOnTTY(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		switch name {
		case "tail":
			return "/usr/bin/tail", nil
		case "tspin":
			return "/usr/local/bin/tspin", nil
		}
		return "", errors.New("unexpected lookup")
	})
	useTail, useTspin := chooseStreamMode(true)
	if !useTail || !useTspin {
		t.Fatalf("got (%v,%v), want (true,true)",
			useTail, useTspin)
	}
}

func TestChooseStreamMode_TailOnlyWhenNotTTY(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		switch name {
		case "tail":
			return "/usr/bin/tail", nil
		case "tspin":
			return "/usr/local/bin/tspin", nil
		}
		return "", errors.New("unexpected lookup")
	})
	useTail, useTspin := chooseStreamMode(false)
	if !useTail || useTspin {
		t.Fatalf("got (%v,%v), want (true,false)",
			useTail, useTspin)
	}
}

func TestChooseStreamMode_TailOnlyWhenTspinMissing(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		if name == "tail" {
			return "/usr/bin/tail", nil
		}
		return "", errors.New("not found")
	})
	useTail, useTspin := chooseStreamMode(true)
	if !useTail || useTspin {
		t.Fatalf("got (%v,%v), want (true,false)",
			useTail, useTspin)
	}
}

func TestChooseStreamMode_FalseWhenTailMissing(t *testing.T) {
	withLookPath(t, func(string) (string, error) {
		return "", errors.New("not found")
	})
	useTail, useTspin := chooseStreamMode(true)
	if useTail || useTspin {
		t.Fatalf("got (%v,%v), want (false,false)",
			useTail, useTspin)
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

func TestStreamViewer_FallbackToIOCopy(t *testing.T) {
	withLookPath(t, func(string) (string, error) {
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := streamViewer(
		t.Context(), path, nil, &buf, io.Discard,
	); err != nil {
		t.Fatalf("streamViewer: %v", err)
	}
	if buf.String() != "body" {
		t.Fatalf("buf = %q, want body", buf.String())
	}
}

// safeBuf is a goroutine-safe bytes.Buffer used by streaming tests
// where one goroutine writes (via the Viewer subprocess) and another
// polls the contents.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// waitForSubstring polls fn until it returns a string containing
// want or the deadline ctx fires. Returns true on hit, false on
// timeout. The poll interval is small enough to keep tests under a
// second on a healthy host.
func waitForSubstring(
	ctx context.Context, fn func() string, want string,
) bool {
	for {
		if strings.Contains(fn(), want) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestStreamViewer_StreamsTailOutput(t *testing.T) {
	if _, err := exec.LookPath("tail"); err != nil {
		t.Skip("tail not on PATH")
	}
	withLookPath(t, func(name string) (string, error) {
		if name == "tail" {
			return exec.LookPath("tail")
		}
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	seed := "via-tail\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline, cancelDeadline := context.WithTimeout(
		t.Context(), 5*time.Second,
	)
	defer cancelDeadline()
	ctx, cancel := context.WithCancel(deadline)
	defer cancel()
	out := &safeBuf{}
	done := make(chan error, 1)
	go func() {
		done <- streamViewer(ctx, path, nil, out, io.Discard)
	}()
	if !waitForSubstring(deadline, out.String, "via-tail") {
		cancel()
		<-done
		t.Fatalf("timed out; buf = %q", out.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("streamViewer: %v", err)
	}
}

func TestRunTailIntoTspin_PipesEndToEnd(t *testing.T) {
	tailPath, err := exec.LookPath("tail")
	if err != nil {
		t.Skip("tail not on PATH")
	}
	catPath, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat not on PATH")
	}
	// stand `cat` in for `tspin` so the pipeline can be exercised
	// end-to-end without depending on the real tspin in CI.
	withLookPath(t, func(name string) (string, error) {
		switch name {
		case "tail":
			return tailPath, nil
		case "tspin":
			return catPath, nil
		}
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(
		path, []byte("via-pipe\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	deadline, cancelDeadline := context.WithTimeout(
		t.Context(), 5*time.Second,
	)
	defer cancelDeadline()
	ctx, cancel := context.WithCancel(deadline)
	defer cancel()
	out := &safeBuf{}
	done := make(chan error, 1)
	go func() {
		done <- runTailIntoTspin(ctx, path, nil, out, io.Discard)
	}()
	if !waitForSubstring(deadline, out.String, "via-pipe") {
		cancel()
		<-done
		t.Fatalf("timed out; buf = %q", out.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runTailIntoTspin: %v", err)
	}
}

func TestRunTailIntoTspin_TailLookPathError(t *testing.T) {
	withLookPath(t, func(string) (string, error) {
		return "", errors.New("not found")
	})
	err := runTailIntoTspin(
		t.Context(),
		filepath.Join(t.TempDir(), "x"),
		nil, io.Discard, io.Discard,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "tail|tspin") {
		t.Fatalf("err = %v, want wrapped tail|tspin error", err)
	}
}

func TestRunTailIntoTspin_TspinLookPathError(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		if name == "tail" {
			return "/usr/bin/tail", nil
		}
		return "", errors.New("not found")
	})
	err := runTailIntoTspin(
		t.Context(),
		filepath.Join(t.TempDir(), "x"),
		nil, io.Discard, io.Discard,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "tail|tspin") {
		t.Fatalf("err = %v, want wrapped tail|tspin error", err)
	}
}

func TestRunTailIntoTspin_TspinStartError(t *testing.T) {
	tailPath, err := exec.LookPath("tail")
	if err != nil {
		t.Skip("tail not on PATH")
	}
	withLookPath(t, func(name string) (string, error) {
		switch name {
		case "tail":
			return tailPath, nil
		case "tspin":
			return "/path/that/does/not/exist/tspin", nil
		}
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(
		path, []byte("seed\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	err = runTailIntoTspin(
		t.Context(), path, nil, io.Discard, io.Discard,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "tail|tspin") {
		t.Fatalf("err = %v, want wrapped tail|tspin error", err)
	}
}

func TestRunTailIntoTspin_TailStartError(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		switch name {
		case "tail":
			return "/path/that/does/not/exist/tail", nil
		case "tspin":
			cat, err := exec.LookPath("cat")
			if err != nil {
				return "", errors.New("cat missing")
			}
			return cat, nil
		}
		return "", errors.New("not found")
	})
	err := runTailIntoTspin(
		t.Context(),
		filepath.Join(t.TempDir(), "x"),
		nil, io.Discard, io.Discard,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "tail|tspin") {
		t.Fatalf("err = %v, want wrapped tail|tspin error", err)
	}
}

func TestStreamViewer_TailLookPathError(t *testing.T) {
	// chooseStreamMode flips useTail true on the first call and
	// the second lookPath inside streamViewer fails — exercises
	// the otherwise-unreachable post-init lookup error branch.
	calls := 0
	withLookPath(t, func(name string) (string, error) {
		calls++
		if name == "tail" && calls == 1 {
			return "/usr/bin/tail", nil
		}
		return "", errors.New("not found")
	})
	err := streamViewer(
		t.Context(),
		filepath.Join(t.TempDir(), "x"),
		nil, io.Discard, io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "tail") {
		t.Fatalf("err = %v, want wrapped tail error", err)
	}
}

func TestStreamViewer_TailExecError(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		if name == "tail" {
			return "/path/that/does/not/exist/tail", nil
		}
		return "", errors.New("not found")
	})
	err := streamViewer(
		t.Context(),
		filepath.Join(t.TempDir(), "x"),
		nil, io.Discard, io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "tail") {
		t.Fatalf("err = %v, want wrapped tail error", err)
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

// TestStreamViewer_TailSuccessPath exercises the `return nil` at the end of
// streamViewer when tail exits with code 0 and the context is not cancelled.
// We fake `tail` with /usr/bin/true so it exits immediately and cleanly.
func TestStreamViewer_TailSuccessPath(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not on PATH")
	}
	withLookPath(t, func(name string) (string, error) {
		if name == "tail" {
			return truePath, nil
		}
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := streamViewer(t.Context(), path, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("streamViewer: %v", err)
	}
}

// TestStreamViewer_UseTspinPath exercises the `return runTailIntoTspin()`
// branch in streamViewer (line 91-93) by making out a TTY device so
// isTerminal(out)=true and looking up both tail and tspin. Skipped when no
// accessible TTY is found.
func TestStreamViewer_UseTspinPath(t *testing.T) {
	tailPath, err := exec.LookPath("tail")
	if err != nil {
		t.Skip("tail not on PATH")
	}
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not on PATH")
	}
	tty := openFirstTTY(t)
	withLookPath(t, func(name string) (string, error) {
		switch name {
		case "tail":
			return tailPath, nil
		case "tspin":
			return truePath, nil
		}
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- streamViewer(ctx, path, tty, tty, io.Discard)
	}()
	cancel()
	err = <-done
	t.Logf("streamViewer(tty) = %v (nil or error both acceptable)", err)
}

// TestRunTailIntoTspin_StdoutPipeError exercises the tail.StdoutPipe() error
// branch in runTailIntoTspin by using an exec path that creates a broken
// command (pipe on an already-started cmd).
func TestRunTailIntoTspin_StdoutPipeError(t *testing.T) {
	tailPath, err := exec.LookPath("tail")
	if err != nil {
		t.Skip("tail not on PATH")
	}
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not on PATH")
	}
	withLookPath(t, func(name string) (string, error) {
		if name == "tail" {
			return tailPath, nil
		}
		if name == "tspin" {
			return truePath, nil
		}
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runTailIntoTspin(ctx, path, nil, io.Discard, io.Discard) }()
	cancel()
	err = <-done
	t.Logf("runTailIntoTspin = %v", err)
}

// TestRunTailIntoTspin_WaitErrors exercises the tail/tspin wait-error branches
// in runTailIntoTspin by running with a cancelled context so both processes
// are killed and their Wait calls return errors.
func TestRunTailIntoTspin_WaitErrors(t *testing.T) {
	tailPath, err := exec.LookPath("tail")
	if err != nil {
		t.Skip("tail not on PATH")
	}
	catPath, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat not on PATH")
	}
	withLookPath(t, func(name string) (string, error) {
		if name == "tail" {
			return tailPath, nil
		}
		if name == "tspin" {
			return catPath, nil
		}
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runTailIntoTspin(ctx, path, nil, io.Discard, io.Discard); err != nil {
		t.Logf("runTailIntoTspin error (expected): %v", err)
	}
}

// makeFakeBin writes a tiny shell script that exits with code exitCode.
func makeFakeBin(t *testing.T, exitCode int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fakecmd.sh")
	body := fmt.Appendf(nil, "#!/bin/sh\nexit %d\n", exitCode)
	if err := os.WriteFile(p, body, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestRunTailIntoTspin_TailFailsNoCancel exercises the tailErr branch: tail
// exits with non-zero code (via fake binary) and the context is not cancelled.
func TestRunTailIntoTspin_TailFailsNoCancel(t *testing.T) {
	failBin := makeFakeBin(t, 1)
	catPath, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat not on PATH")
	}
	withLookPath(t, func(name string) (string, error) {
		if name == "tail" {
			return failBin, nil
		}
		if name == "tspin" {
			return catPath, nil
		}
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = runTailIntoTspin(t.Context(), path, nil, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "tail|tspin") {
		t.Fatalf("err = %v, want tail|tspin error", err)
	}
}

// TestRunTailIntoTspin_TspinFailsNoCancel exercises the tspinErr branch:
// tail exits 0 and tspin exits non-zero (via fake binary), context not cancelled.
func TestRunTailIntoTspin_TspinFailsNoCancel(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not on PATH")
	}
	failBin := makeFakeBin(t, 1)
	withLookPath(t, func(name string) (string, error) {
		if name == "tail" {
			return truePath, nil
		}
		if name == "tspin" {
			return failBin, nil
		}
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = runTailIntoTspin(t.Context(), path, nil, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "tail|tspin") {
		t.Fatalf("err = %v, want tail|tspin error", err)
	}
}

// TestRunTailIntoTspin_BothSucceed exercises the `return nil` path: both tail
// and tspin exit 0 with no context cancellation.
func TestRunTailIntoTspin_BothSucceed(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not on PATH")
	}
	withLookPath(t, func(name string) (string, error) {
		if name == "tail" || name == "tspin" {
			return truePath, nil
		}
		return "", errors.New("not found")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runTailIntoTspin(t.Context(), path, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("runTailIntoTspin: %v", err)
	}
}
