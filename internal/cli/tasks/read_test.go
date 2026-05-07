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

// readLeaf names the leaf under test so the same harness drives
// requirements / plan without duplicating the table.
type readLeaf struct {
	name      string
	filename  string
	run       func(context.Context, ReadOptions) error
	viperKey  string
	envName   string
	cmdArgv   []string
}

func readLeaves() []readLeaf {
	return []readLeaf{
		{
			name:     "requirements",
			filename: tasks.RequirementsFileName,
			run:      RunReadRequirements,
			viperKey: "tasks.read.requirements.from_task",
			envName:  "TASKS_READ_REQUIREMENTS_FROM_TASK",
			cmdArgv:  []string{"read", "requirements"},
		},
		{
			name:     "plan",
			filename: tasks.PlanFileName,
			run:      RunReadPlan,
			viperKey: "tasks.read.plan.from_task",
			envName:  "TASKS_READ_PLAN_FROM_TASK",
			cmdArgv:  []string{"read", "plan"},
		},
	}
}

func TestRunRead_DirectIDHappyPath(t *testing.T) {
	for _, leaf := range readLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			seedTaskWithFile(t, "id-direct", "x",
				leaf.filename, "body")
			viewer := &fakeViewer{}
			err = leaf.run(context.Background(), ReadOptions{
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

func TestRunRead_DirectIDUnknown(t *testing.T) {
	for _, leaf := range readLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			testutil.Init(t)
			viewer := &fakeViewer{}
			var stdout bytes.Buffer
			err := leaf.run(context.Background(), ReadOptions{
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

func TestRunRead_DirectIDFileMissing(t *testing.T) {
	for _, leaf := range readLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			seedTaskWithFile(t, "id-no-file", "x", "", "")
			viewer := &fakeViewer{}
			var stdout bytes.Buffer
			err := leaf.run(context.Background(), ReadOptions{
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

func TestRunRead_NoIDEmptyStore(t *testing.T) {
	for _, leaf := range readLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			testutil.Init(t)
			viewer := &fakeViewer{}
			ui := &fakeUI{}
			var stdout bytes.Buffer
			err := leaf.run(context.Background(), ReadOptions{
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

func TestRunRead_NoIDPickerHappyPath(t *testing.T) {
	for _, leaf := range readLeaves() {
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
			err = leaf.run(context.Background(), ReadOptions{
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

func TestRunRead_NoIDPickerAbort(t *testing.T) {
	for _, leaf := range readLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			seedTaskWithFile(t, "id-abort", "x",
				leaf.filename, "body")
			ui := &fakeUI{}
			viewer := &fakeViewer{}
			var stdout bytes.Buffer
			err := leaf.run(context.Background(), ReadOptions{
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

func TestRunRead_ViewerErrorPropagates(t *testing.T) {
	leaf := readLeaves()[0]
	t.Chdir(t.TempDir())
	seedTaskWithFile(t, "id-vw-err", "x",
		leaf.filename, "body")
	boom := errors.New("viewer boom")
	err := leaf.run(context.Background(), ReadOptions{
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

func TestNewReadCmd_RegistersLeaves(t *testing.T) {
	parent := newReadCmd()
	if parent.Use != "read" {
		t.Fatalf("Use = %q, want read", parent.Use)
	}
	want := map[string]bool{
		"requirements": false,
		"plan":         false,
	}
	for _, sub := range parent.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, ok := range want {
		if !ok {
			t.Fatalf("read leaf %q was not registered", name)
		}
	}
}

func TestNew_HasReadSubcommand(t *testing.T) {
	cmd := New()
	for _, child := range cmd.Commands() {
		if child.Name() == "read" {
			return
		}
	}
	t.Fatal("expected `read` to be registered on `j tasks`")
}

func TestNewReadLeaves_FlagAndEnvBindings(t *testing.T) {
	for _, leaf := range readLeaves() {
		t.Run(leaf.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			parent := newReadCmd()
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
			_ = newReadCmd()
			if v := viper.GetString(
				leaf.viperKey,
			); v != "via-env" {
				t.Fatalf("%s = %q, want via-env",
					leaf.viperKey, v)
			}
		})
	}
}

func TestRunReadCmd_RunE_DirectIDViaEnv(t *testing.T) {
	for _, leaf := range readLeaves() {
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

func TestReadOptions_WithDefaults_FillsAllNilStreams(t *testing.T) {
	o := ReadOptions{}.withDefaults()
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

func TestReadOptions_WithDefaults_KeepsProvided(t *testing.T) {
	customUI := &fakeUI{}
	customViewer := withFakeViewer(&fakeViewer{})
	o := ReadOptions{
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
