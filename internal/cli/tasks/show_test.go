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

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// showLeaf names the leaf under test so the same harness drives
// requirements / plan without duplicating the table.
type showLeaf struct {
	name      string
	filename  string
	run       func(context.Context, ShowOptions) error
	viperKey  string
	envName   string
	cmdArgv   []string
}

func showLeaves() []showLeaf {
	return []showLeaf{
		{
			name:     "requirements",
			filename: tasks.RequirementsFileName,
			run:      RunShowRequirements,
			viperKey: "tasks.show.requirements.from_task",
			envName:  "TASKS_SHOW_REQUIREMENTS_FROM_TASK",
			cmdArgv:  []string{"show", "requirements"},
		},
		{
			name:     "plan",
			filename: tasks.PlanFileName,
			run:      RunShowPlan,
			viperKey: "tasks.show.plan.from_task",
			envName:  "TASKS_SHOW_PLAN_FROM_TASK",
			cmdArgv:  []string{"show", "plan"},
		},
	}
}

// --- RunShow (task.toml) tests ---

func TestRunShow_DirectIDHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTaskWithFile(t, "id-show", "x", "", "")
	viewer := &fakeViewer{}
	err = RunShow(context.Background(), ShowOptions{
		TaskID: "id-show",
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{},
		Viewer: withFakeViewer(viewer),
	})
	if err != nil {
		t.Fatalf("RunShow: %v", err)
	}
	if viewer.calls != 1 {
		t.Fatalf("Viewer calls = %d, want 1", viewer.calls)
	}
	want := filepath.Join(
		cwd, ".j", tasks.DirName, "id-show", tasks.TaskFileName,
	)
	if viewer.lastPath != want {
		t.Fatalf("Viewer path = %q, want %q",
			viewer.lastPath, want)
	}
}

func TestRunShow_DirectIDUnknown(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	viewer := &fakeViewer{}
	var stdout bytes.Buffer
	err := RunShow(context.Background(), ShowOptions{
		TaskID: "ghost",
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     &fakeUI{},
		Viewer: withFakeViewer(viewer),
	})
	if err != nil {
		t.Fatalf("RunShow: %v", err)
	}
	if viewer.calls != 0 {
		t.Fatalf("Viewer calls = %d, want 0", viewer.calls)
	}
	line := strings.TrimRight(stdout.String(), "\n")
	if line != noTaskMessage {
		t.Fatalf("stdout = %q, want %q", line, noTaskMessage)
	}
}

func TestRunShow_NoIDEmptyStore(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	viewer := &fakeViewer{}
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunShow(context.Background(), ShowOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
		Viewer: withFakeViewer(viewer),
	})
	if err != nil {
		t.Fatalf("RunShow: %v", err)
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

func TestRunShow_NoIDPickerHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedTaskWithFile(t, "id-pk", "x", "", "")
	ui := &fakeUI{pickReturn: "id-pk"}
	viewer := &fakeViewer{}
	err = RunShow(context.Background(), ShowOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     ui,
		Viewer: withFakeViewer(viewer),
	})
	if err != nil {
		t.Fatalf("RunShow: %v", err)
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

func TestRunShow_NoIDPickerAbort(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-abort", "x", "", "")
	ui := &fakeUI{}
	viewer := &fakeViewer{}
	var stdout bytes.Buffer
	err := RunShow(context.Background(), ShowOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		UI:     ui,
		Viewer: withFakeViewer(viewer),
	})
	if err != nil {
		t.Fatalf("RunShow: %v", err)
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

func TestRunShow_ViewerErrorPropagates(t *testing.T) {
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-vw-err", "x", "", "")
	boom := errors.New("viewer boom")
	err := RunShow(context.Background(), ShowOptions{
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

// --- Show leaves (requirements / plan) tests ---

func TestRunShowLeaf_DirectIDHappyPath(t *testing.T) {
	for _, leaf := range showLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			seedTaskWithFile(t, "id-direct", "x",
				leaf.filename, "body")
			viewer := &fakeViewer{}
			err = leaf.run(context.Background(), ShowOptions{
				TaskID: "id-direct",
				Stdout: io.Discard,
				Stderr: io.Discard,
				UI:     &fakeUI{},
				Viewer: withFakeViewer(viewer),
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if viewer.calls != 1 {
				t.Fatalf("Viewer calls = %d, want 1",
					viewer.calls)
			}
			want := filepath.Join(
				cwd, ".j", tasks.DirName, "id-direct",
				leaf.filename,
			)
			if viewer.lastPath != want {
				t.Fatalf("Viewer path = %q, want %q",
					viewer.lastPath, want)
			}
		})
	}
}

func TestRunShowLeaf_DirectIDUnknown(t *testing.T) {
	for _, leaf := range showLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			testutil.Init(t)
			viewer := &fakeViewer{}
			var stdout bytes.Buffer
			err := leaf.run(context.Background(), ShowOptions{
				TaskID: "ghost",
				Stdout: &stdout,
				Stderr: io.Discard,
				UI:     &fakeUI{},
				Viewer: withFakeViewer(viewer),
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if viewer.calls != 0 {
				t.Fatalf("Viewer calls = %d, want 0",
					viewer.calls)
			}
			line := strings.TrimRight(stdout.String(), "\n")
			if line != noTaskMessage {
				t.Fatalf("stdout = %q, want %q",
					line, noTaskMessage)
			}
		})
	}
}

func TestRunShowLeaf_DirectIDFileMissing(t *testing.T) {
	for _, leaf := range showLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			seedTaskWithFile(t, "id-no-file", "x", "", "")
			viewer := &fakeViewer{}
			var stdout bytes.Buffer
			err := leaf.run(context.Background(), ShowOptions{
				TaskID: "id-no-file",
				Stdout: &stdout,
				Stderr: io.Discard,
				UI:     &fakeUI{},
				Viewer: withFakeViewer(viewer),
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if viewer.calls != 0 {
				t.Fatalf("Viewer calls = %d, want 0",
					viewer.calls)
			}
			want := "J: " + leaf.filename +
				" not found for task id-no-file"
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout = %q, want substring %q",
					stdout.String(), want)
			}
		})
	}
}

func TestRunShowLeaf_NoIDEmptyStore(t *testing.T) {
	for _, leaf := range showLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			testutil.Init(t)
			viewer := &fakeViewer{}
			ui := &fakeUI{}
			var stdout bytes.Buffer
			err := leaf.run(context.Background(), ShowOptions{
				Stdout: &stdout,
				Stderr: io.Discard,
				UI:     ui,
				Viewer: withFakeViewer(viewer),
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if viewer.calls != 0 || ui.pickCalls != 0 {
				t.Fatalf(
					"viewer=%d picker=%d, want both 0",
					viewer.calls, ui.pickCalls,
				)
			}
			line := strings.TrimRight(stdout.String(), "\n")
			if line != emptyMessage {
				t.Fatalf("stdout = %q, want %q",
					line, emptyMessage)
			}
		})
	}
}

func TestRunShowLeaf_NoIDPickerHappyPath(t *testing.T) {
	for _, leaf := range showLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			seedTaskWithFile(t, "id-pk", "x",
				leaf.filename, "body")
			ui := &fakeUI{pickReturn: "id-pk"}
			viewer := &fakeViewer{}
			err = leaf.run(context.Background(), ShowOptions{
				Stdout: io.Discard,
				Stderr: io.Discard,
				UI:     ui,
				Viewer: withFakeViewer(viewer),
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if ui.pickCalls != 1 {
				t.Fatalf("PickTask calls = %d, want 1",
					ui.pickCalls)
			}
			if viewer.calls != 1 {
				t.Fatalf("Viewer calls = %d, want 1",
					viewer.calls)
			}
			want := filepath.Join(
				cwd, ".j", tasks.DirName, "id-pk",
				leaf.filename,
			)
			if viewer.lastPath != want {
				t.Fatalf("Viewer path = %q, want %q",
					viewer.lastPath, want)
			}
		})
	}
}

func TestRunShowLeaf_NoIDPickerAbort(t *testing.T) {
	for _, leaf := range showLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			seedTaskWithFile(t, "id-abort", "x",
				leaf.filename, "body")
			ui := &fakeUI{}
			viewer := &fakeViewer{}
			var stdout bytes.Buffer
			err := leaf.run(context.Background(), ShowOptions{
				Stdout: &stdout,
				Stderr: io.Discard,
				UI:     ui,
				Viewer: withFakeViewer(viewer),
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if ui.pickCalls != 1 {
				t.Fatalf("PickTask calls = %d, want 1",
					ui.pickCalls)
			}
			if viewer.calls != 0 {
				t.Fatalf("Viewer calls = %d, want 0",
					viewer.calls)
			}
			if stdout.String() != "" {
				t.Fatalf("stdout = %q, want empty",
					stdout.String())
			}
		})
	}
}

func TestRunShowLeaf_ViewerErrorPropagates(t *testing.T) {
	leaf := showLeaves()[0]
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-vw-err", "x",
		leaf.filename, "body")
	boom := errors.New("viewer boom")
	err := leaf.run(context.Background(), ShowOptions{
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

// --- Command shape tests ---

func TestNewShowCmd_RegistersLeavesAndParentRunE(t *testing.T) {
	cmd := newShowCmd()
	if cmd.Use != "show" {
		t.Fatalf("Use = %q, want show", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Fatal("parent show RunE is nil, want task.toml renderer")
	}
	want := map[string]bool{
		"requirements": false,
		"plan":         false,
	}
	for _, sub := range cmd.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, ok := range want {
		if !ok {
			t.Fatalf("show leaf %q was not registered", name)
		}
	}
}

func TestNew_HasShowSubcommand(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == "show" {
			return
		}
	}
	t.Fatal("expected `show` to be registered on `j tasks`")
}

func TestNew_ReadSubcommandIsGone(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == "read" {
			t.Fatal("expected `read` to be removed from `j tasks`")
		}
	}
}

// --- Flag and env binding tests ---

func TestNewShowCmd_FlagAndEnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newShowCmd()
	f := cmd.Flags().Lookup("from-task")
	if f == nil || f.DefValue != "" {
		t.Fatalf("--from-task flag = %v", f)
	}
	if v := viper.GetString("tasks.show.from_task"); v != "" {
		t.Fatalf("default = %q", v)
	}
	t.Setenv("TASKS_SHOW_FROM_TASK", "via-env")
	_ = newShowCmd()
	if v := viper.GetString("tasks.show.from_task"); v != "via-env" {
		t.Fatalf("after env = %q, want via-env", v)
	}
}

func TestNewShowLeaves_FlagAndEnvBindings(t *testing.T) {
	for _, leaf := range showLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			parent := newShowCmd()
			var sub *cobra.Command
			for _, c := range parent.Commands() {
				if c.Name() == leaf.name {
					sub = c
					break
				}
			}
			if sub == nil {
				t.Fatalf("leaf %q missing", leaf.name)
			}
			f := sub.Flags().Lookup("from-task")
			if f == nil {
				t.Fatal("--from-task flag not registered")
			}
			if f.DefValue != "" {
				t.Fatalf("--from-task default = %q",
					f.DefValue)
			}
			if v := viper.GetString(leaf.viperKey); v != "" {
				t.Fatalf("%s default = %q",
					leaf.viperKey, v)
			}
			t.Setenv(leaf.envName, "via-env")
			_ = newShowCmd()
			if v := viper.GetString(
				leaf.viperKey,
			); v != "via-env" {
				t.Fatalf("%s = %q, want via-env",
					leaf.viperKey, v)
			}
		})
	}
}

// --- Push-through-execute tests ---

func TestRunShowCmd_RunE_DirectIDViaEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	testutil.Init(t)
	t.Setenv("TASKS_SHOW_FROM_TASK", "ghost")
	root := New()
	var stdout bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetContext(context.Background())
	root.SetArgs([]string{"show"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	line := strings.TrimRight(stdout.String(), "\n")
	if line != noTaskMessage {
		t.Fatalf("stdout = %q, want %q", line, noTaskMessage)
	}
}

func TestRunShowLeafCmd_RunE_DirectIDViaEnv(t *testing.T) {
	for _, leaf := range showLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Chdir(t.TempDir())
			testutil.Init(t)
			t.Setenv(leaf.envName, "ghost")
			root := New()
			var stdout bytes.Buffer
			root.SetIn(strings.NewReader(""))
			root.SetOut(&stdout)
			root.SetErr(io.Discard)
			root.SetContext(context.Background())
			root.SetArgs(leaf.cmdArgv)
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			line := strings.TrimRight(stdout.String(), "\n")
			if line != noTaskMessage {
				t.Fatalf("stdout = %q, want %q",
					line, noTaskMessage)
			}
		})
	}
}

// --- ShowOptions.withDefaults tests ---

func TestShowOptions_WithDefaults_FillsAllNilStreams(t *testing.T) {
	o := ShowOptions{}.withDefaults()
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

func TestShowOptions_WithDefaults_KeepsProvided(t *testing.T) {
	customUI := &fakeUI{}
	customViewer := withFakeViewer(&fakeViewer{})
	o := ShowOptions{
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

// Ensure `j tasks read` returns cobra "unknown command".
func TestNew_ReadUnknownCommand(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	root := New()
	root.SetIn(strings.NewReader(""))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetContext(context.Background())
	root.SetArgs([]string{"read"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected `j tasks read` to fail as unknown command")
	}
}
