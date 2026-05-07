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

func TestRunLogs_DirectIDHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTaskWithFile(t, "id-log", "x",
		tasks.AgentLogFileName, "log line\n")
	tailer := &fakeTailer{}
	err = RunLogs(context.Background(), LogsOptions{
		TaskID: "id-log",
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{},
		Tailer: withFakeTailer(tailer),
	})
	if err != nil {
		t.Fatalf("RunLogs: %v", err)
	}
	if tailer.calls != 1 {
		t.Fatalf("Tailer calls = %d, want 1", tailer.calls)
	}
	want := filepath.Join(
		cwd, ".j", tasks.DirName, "id-log",
		tasks.AgentLogFileName,
	)
	if tailer.lastPath != want {
		t.Fatalf("Tailer path = %q, want %q",
			tailer.lastPath, want)
	}
}

func TestRunLogs_DirectIDUnknown(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	tailer := &fakeTailer{}
	var stdout bytes.Buffer
	err := RunLogs(context.Background(), LogsOptions{
		TaskID: "ghost",
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     &fakeUI{},
		Tailer: withFakeTailer(tailer),
	})
	if err != nil {
		t.Fatalf("RunLogs: %v", err)
	}
	if tailer.calls != 0 {
		t.Fatalf("Tailer calls = %d, want 0", tailer.calls)
	}
	line := strings.TrimRight(stdout.String(), "\n")
	if line != noTaskMessage {
		t.Fatalf("stdout = %q, want %q", line, noTaskMessage)
	}
}

func TestRunLogs_DirectIDFileMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-no-log", "x", "", "")
	tailer := &fakeTailer{}
	var stdout bytes.Buffer
	err := RunLogs(context.Background(), LogsOptions{
		TaskID: "id-no-log",
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     &fakeUI{},
		Tailer: withFakeTailer(tailer),
	})
	if err != nil {
		t.Fatalf("RunLogs: %v", err)
	}
	if tailer.calls != 0 {
		t.Fatalf("Tailer calls = %d, want 0", tailer.calls)
	}
	want := "J: " + tasks.AgentLogFileName +
		" not found for task id-no-log"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want substring %q",
			stdout.String(), want)
	}
}

func TestRunLogs_NoIDEmptyStore(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	tailer := &fakeTailer{}
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunLogs(context.Background(), LogsOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
		Tailer: withFakeTailer(tailer),
	})
	if err != nil {
		t.Fatalf("RunLogs: %v", err)
	}
	if tailer.calls != 0 || ui.pickCalls != 0 {
		t.Fatalf("tailer=%d picker=%d, want 0,0",
			tailer.calls, ui.pickCalls)
	}
	line := strings.TrimRight(stdout.String(), "\n")
	if line != emptyMessage {
		t.Fatalf("stdout = %q, want %q", line, emptyMessage)
	}
}

func TestRunLogs_NoIDPickerHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTaskWithFile(t, "id-pk", "x",
		tasks.AgentLogFileName, "body")
	ui := &fakeUI{pickReturn: "id-pk"}
	tailer := &fakeTailer{}
	err = RunLogs(context.Background(), LogsOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     ui,
		Tailer: withFakeTailer(tailer),
	})
	if err != nil {
		t.Fatalf("RunLogs: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if tailer.calls != 1 {
		t.Fatalf("Tailer calls = %d, want 1", tailer.calls)
	}
	want := filepath.Join(
		cwd, ".j", tasks.DirName, "id-pk",
		tasks.AgentLogFileName,
	)
	if tailer.lastPath != want {
		t.Fatalf("Tailer path = %q, want %q",
			tailer.lastPath, want)
	}
}

func TestRunLogs_NoIDPickerAbort(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-abort", "x",
		tasks.AgentLogFileName, "")
	ui := &fakeUI{}
	tailer := &fakeTailer{}
	var stdout bytes.Buffer
	err := RunLogs(context.Background(), LogsOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
		Tailer: withFakeTailer(tailer),
	})
	if err != nil {
		t.Fatalf("RunLogs: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if tailer.calls != 0 {
		t.Fatalf("Tailer calls = %d, want 0", tailer.calls)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunLogs_TailerErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-t-err", "x",
		tasks.AgentLogFileName, "")
	boom := errors.New("tailer boom")
	err := RunLogs(context.Background(), LogsOptions{
		TaskID: "id-t-err",
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{},
		Tailer: withFakeTailer(&fakeTailer{returnErr: boom}),
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestNew_HasLogsSubcommand(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == "logs" {
			return
		}
	}
	t.Fatal("expected `logs` to be registered on `j tasks`")
}

func TestNewLogsCmd_FlagAndEnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newLogsCmd()
	if cmd.Use != "logs" {
		t.Fatalf("Use = %q, want logs", cmd.Use)
	}
	f := cmd.Flags().Lookup("from-task")
	if f == nil || f.DefValue != "" {
		t.Fatalf("--from-task flag = %v", f)
	}
	if v := viper.GetString("tasks.logs.from_task"); v != "" {
		t.Fatalf("default = %q", v)
	}
	t.Setenv("TASKS_LOGS_FROM_TASK", "via-env")
	_ = newLogsCmd()
	if v := viper.GetString(
		"tasks.logs.from_task",
	); v != "via-env" {
		t.Fatalf("after env = %q, want via-env", v)
	}
}

func TestNewLogsCmd_RunE_DirectIDViaEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	testutil.Init(t)
	t.Setenv("TASKS_LOGS_FROM_TASK", "ghost")
	root := New()
	var stdout bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetContext(context.Background())
	root.SetArgs([]string{"logs"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	line := strings.TrimRight(stdout.String(), "\n")
	if line != noTaskMessage {
		t.Fatalf("stdout = %q, want %q", line, noTaskMessage)
	}
}

func TestLogsOptions_WithDefaults_FillsAllNilStreams(t *testing.T) {
	o := LogsOptions{}.withDefaults()
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
	if o.Tailer == nil {
		t.Error("Tailer was not defaulted")
	}
}

func TestLogsOptions_WithDefaults_KeepsProvided(t *testing.T) {
	customUI := &fakeUI{}
	customTailer := withFakeTailer(&fakeTailer{})
	o := LogsOptions{
		UI:     customUI,
		Tailer: customTailer,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}.withDefaults()
	if o.UI != customUI {
		t.Errorf("UI = %v, want custom", o.UI)
	}
	if o.Tailer == nil {
		t.Error("Tailer was wiped")
	}
}
