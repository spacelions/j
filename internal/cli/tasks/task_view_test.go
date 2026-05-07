package tasks

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

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

func TestRunTaskView_DirectIDHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// PutTask writes task.toml automatically; no extra file needed.
	seedTaskWithFile(t, "id-tv", "x", "", "")
	viewer := &fakeViewer{}
	err = RunTaskView(context.Background(), TaskViewOptions{
		TaskID: "id-tv",
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{},
		Viewer: withFakeViewer(viewer),
	})
	if err != nil {
		t.Fatalf("RunTaskView: %v", err)
	}
	if viewer.calls != 1 {
		t.Fatalf("Viewer calls = %d, want 1", viewer.calls)
	}
	want := filepath.Join(
		cwd, ".j", tasks.DirName, "id-tv", tasks.TaskFileName,
	)
	if viewer.lastPath != want {
		t.Fatalf("Viewer path = %q, want %q",
			viewer.lastPath, want)
	}
}

func TestRunTaskView_DirectIDUnknown(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	viewer := &fakeViewer{}
	var stdout bytes.Buffer
	err := RunTaskView(context.Background(), TaskViewOptions{
		TaskID: "ghost",
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     &fakeUI{},
		Viewer: withFakeViewer(viewer),
	})
	if err != nil {
		t.Fatalf("RunTaskView: %v", err)
	}
	if viewer.calls != 0 {
		t.Fatalf("Viewer calls = %d, want 0", viewer.calls)
	}
	line := strings.TrimRight(stdout.String(), "\n")
	if line != noTaskMessage {
		t.Fatalf("stdout = %q, want %q", line, noTaskMessage)
	}
}

func TestRunTaskView_NoIDEmptyStore(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	viewer := &fakeViewer{}
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunTaskView(context.Background(), TaskViewOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
		Viewer: withFakeViewer(viewer),
	})
	if err != nil {
		t.Fatalf("RunTaskView: %v", err)
	}
	if viewer.calls != 0 || ui.pickCalls != 0 {
		t.Fatalf("viewer=%d picker=%d, want 0,0",
			viewer.calls, ui.pickCalls)
	}
	line := strings.TrimRight(stdout.String(), "\n")
	if line != emptyMessage {
		t.Fatalf("stdout = %q, want %q", line, emptyMessage)
	}
}

func TestRunTaskView_NoIDPickerHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTaskWithFile(t, "id-pk", "x", "", "")
	ui := &fakeUI{pickReturn: "id-pk"}
	viewer := &fakeViewer{}
	err = RunTaskView(context.Background(), TaskViewOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     ui,
		Viewer: withFakeViewer(viewer),
	})
	if err != nil {
		t.Fatalf("RunTaskView: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if viewer.calls != 1 {
		t.Fatalf("Viewer calls = %d, want 1", viewer.calls)
	}
	want := filepath.Join(
		cwd, ".j", tasks.DirName, "id-pk", tasks.TaskFileName,
	)
	if viewer.lastPath != want {
		t.Fatalf("Viewer path = %q, want %q",
			viewer.lastPath, want)
	}
}

func TestRunTaskView_NoIDPickerAbort(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-abort", "x", "", "")
	ui := &fakeUI{}
	viewer := &fakeViewer{}
	var stdout bytes.Buffer
	err := RunTaskView(context.Background(), TaskViewOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
		Viewer: withFakeViewer(viewer),
	})
	if err != nil {
		t.Fatalf("RunTaskView: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if viewer.calls != 0 {
		t.Fatalf("Viewer calls = %d, want 0", viewer.calls)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunTaskView_ViewerErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-vw-err", "x", "", "")
	boom := errors.New("viewer boom")
	err := RunTaskView(context.Background(), TaskViewOptions{
		TaskID: "id-vw-err",
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{},
		Viewer: withFakeViewer(&fakeViewer{returnErr: boom}),
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestNew_HasTaskSubcommand(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == "task" {
			return
		}
	}
	t.Fatal("expected `task` to be registered on `j tasks`")
}

func TestNewTaskViewCmd_FlagAndEnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newTaskViewCmd()
	if cmd.Use != "task" {
		t.Fatalf("Use = %q, want task", cmd.Use)
	}
	f := cmd.Flags().Lookup("from-task")
	if f == nil || f.DefValue != "" {
		t.Fatalf("--from-task flag = %v", f)
	}
	if v := viper.GetString("tasks.task.from_task"); v != "" {
		t.Fatalf("default = %q", v)
	}
	t.Setenv("TASKS_TASK_FROM_TASK", "via-env")
	_ = newTaskViewCmd()
	if v := viper.GetString(
		"tasks.task.from_task",
	); v != "via-env" {
		t.Fatalf("after env = %q, want via-env", v)
	}
}

func TestNewTaskViewCmd_RunE_DirectIDViaEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	testutil.Init(t)
	t.Setenv("TASKS_TASK_FROM_TASK", "ghost")
	root := New()
	var stdout bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetContext(context.Background())
	root.SetArgs([]string{"task"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	line := strings.TrimRight(stdout.String(), "\n")
	if line != noTaskMessage {
		t.Fatalf("stdout = %q, want %q", line, noTaskMessage)
	}
}

func TestTaskViewOptions_WithDefaults_FillsNilStreams(t *testing.T) {
	o := TaskViewOptions{}.withDefaults()
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
	if o.Viewer == nil {
		t.Error("Viewer was not defaulted")
	}
}

func TestTaskViewOptions_WithDefaults_KeepsProvided(t *testing.T) {
	customUI := &fakeUI{}
	customViewer := withFakeViewer(&fakeViewer{})
	o := TaskViewOptions{
		UI:     customUI,
		Viewer: customViewer,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}.withDefaults()
	if o.UI != customUI {
		t.Errorf("UI = %v, want custom", o.UI)
	}
	if o.Viewer == nil {
		t.Error("Viewer was wiped")
	}
}
