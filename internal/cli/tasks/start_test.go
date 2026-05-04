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
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/tasklog"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// writeStartFile writes a markdown task description into the test's
// temp dir and returns its absolute path. Used by the --from-file
// happy path; the source picker tests prefer writeStartFileInCwd
// because mdfile.ListInDir scans the current working directory.
func writeStartFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeStartFileInCwd writes a markdown task description into the
// current working directory under name and returns its basename.
// Used by the source-picker tests so mdfile.ListInDir surfaces the
// file when RunStart drives pickMarkdownTarget.
func writeStartFileInCwd(t *testing.T, name, body string) string {
	t.Helper()
	if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

// noopJBinary writes a tiny shell script that exits zero into the
// test's temp dir and returns its absolute path.
func noopJBinary(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "j-stub.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// readTaskFromBolt opens the per-project tasks DB and returns the
// task row for id (or fails the test if missing).
func readTaskFromBolt(t *testing.T, id string) store.Task {
	t.Helper()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return got
}

// firstSeededTaskID lists every task in the bbolt store and returns
// the only id (failing the test if the count is not exactly one).
func firstSeededTaskID(t *testing.T) string {
	t.Helper()
	rows := allTaskRows(t)
	if len(rows) != 1 {
		t.Fatalf("ListTasks = %d rows, want 1: %+v", len(rows), rows)
	}
	return rows[0].ID
}

// allTaskRows returns every row in the bbolt store; helper for the
// source-picker tests that need to assert "no new row created."
func allTaskRows(t *testing.T) []store.Task {
	t.Helper()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	rows, err := s.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// scriptedStartUI is the in-package fake satisfying StartUI. Each
// method returns a configured value (or error) and records call
// counts so tests can assert which branch fired.
type scriptedStartUI struct {
	source             picker.Source
	sourceErr          error
	sourceCalls        int
	pickedMarkdownPath string
	pickedMarkdownErr  error
	markdownCalls      int
	pickedTaskID       string
	pickedTaskOK       bool
	taskErr            error
	taskCalls          int
}

func (u *scriptedStartUI) SelectSource(_ context.Context, _ []picker.Source) (picker.Source, error) {
	u.sourceCalls++
	return u.source, u.sourceErr
}

func (u *scriptedStartUI) PickMarkdownInCwd(_ context.Context) (string, error) {
	u.markdownCalls++
	if u.pickedMarkdownErr != nil {
		return "", u.pickedMarkdownErr
	}
	return u.pickedMarkdownPath, nil
}

func (u *scriptedStartUI) PickTask(_ context.Context, _ string, _ []store.Task) (string, bool, error) {
	u.taskCalls++
	if u.taskErr != nil {
		return "", false, u.taskErr
	}
	return u.pickedTaskID, u.pickedTaskOK, nil
}

// TestRunStart_HappyPath_FromFile pins the --from-file shortcut.
func TestRunStart_HappyPath_FromFile(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeStartFile(t, "# task\nbody line")
	stub := newScriptedAgent()
	sel := &scriptedSelector{tool: "cursor", model: "sonnet-4"}
	binary := noopJBinary(t)
	var stdout, stderr bytes.Buffer

	start := time.Now()
	err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{stub},
		Selector: sel,
		UI:       &scriptedStartUI{},
		JBinary:  binary,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("RunStart took %v, want <2s for the detached spawn", elapsed)
	}
	if sel.toolCalls != 3 || sel.modelCalls != 3 {
		t.Fatalf("selector calls = (%d, %d), want (3, 3)", sel.toolCalls, sel.modelCalls)
	}
	if !strings.Contains(stdout.String(), "task ") || !strings.Contains(stdout.String(), "tail -f") {
		t.Fatalf("stdout should announce the task: %q", stdout.String())
	}

	id := firstSeededTaskID(t)
	row := readTaskFromBolt(t, id)
	if row.Status != store.StatusPlanning {
		t.Fatalf("Status = %q, want planning", row.Status)
	}
	wantLog := filepath.Join(".j/tasks", id, tasklog.AgentLogFileName)
	if !strings.HasSuffix(row.AgentLogPath, wantLog) {
		t.Fatalf("AgentLogPath = %q, want suffix %q", row.AgentLogPath, wantLog)
	}
	if row.BackgroundPID == 0 {
		t.Fatalf("BackgroundPID = 0, want non-zero (detached child PID)")
	}
	if row.Summary == "" {
		t.Fatalf("Summary should be derived from the markdown body")
	}

	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatal(err)
	}
	reqPath := filepath.Join(tasksDir, id, store.RequirementsFileName)
	body, err := os.ReadFile(reqPath)
	if err != nil {
		t.Fatalf("read requirements.md: %v", err)
	}
	if !strings.Contains(string(body), "body line") {
		t.Fatalf("requirements.md missing user body: %q", body)
	}

	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		tool, model, _ := readAgentBucket(t, bucket)
		if tool != "cursor" || model != "sonnet-4" {
			t.Fatalf("bucket %q = (%q, %q)", bucket, tool, model)
		}
	}
}

// TestRunStart_PrePopulatedSkipsPrompts pins that buckets already
// populated short-circuit the agent-pick prompts.
func TestRunStart_PrePopulatedSkipsPrompts(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	sel := &scriptedSelector{}
	binary := noopJBinary(t)

	err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: sel,
		UI:       &scriptedStartUI{},
		JBinary:  binary,
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if sel.toolCalls != 0 || sel.modelCalls != 0 {
		t.Fatalf("selector calls = (%d, %d), want (0, 0) when buckets are populated", sel.toolCalls, sel.modelCalls)
	}
}

// TestRunStart_NoAgents pins the no-agents-configured branch.
func TestRunStart_NoAgents(t *testing.T) {
	err := RunStart(context.Background(), StartOptions{
		FromFile: "ignored",
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunStart_NoFromFile_PicksMarkdown drives the source picker
// happy path: empty FromFile + UI.SelectSource returns
// SourceMarkdown + UI.PickFromFile returns the staged .md basename.
// A new task row should be seeded just like the --from-file path.
func TestRunStart_NoFromFile_PicksMarkdown(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	writeStartFileInCwd(t, "spec.md", "# task\nbody line")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	ui := &scriptedStartUI{
		source:             picker.SourceMarkdown,
		pickedMarkdownPath: filepath.Join(cwd, "spec.md"),
	}

	err = RunStart(context.Background(), StartOptions{
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: &scriptedSelector{},
		UI:       ui,
		JBinary:  noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if ui.sourceCalls != 1 {
		t.Fatalf("SelectSource calls = %d, want 1", ui.sourceCalls)
	}
	if ui.markdownCalls != 1 {
		t.Fatalf("PickMarkdownInCwd calls = %d, want 1", ui.markdownCalls)
	}
	id := firstSeededTaskID(t)
	row := readTaskFromBolt(t, id)
	if row.Status != store.StatusPlanning {
		t.Fatalf("Status = %q, want planning", row.Status)
	}
	if row.BackgroundPID == 0 {
		t.Fatalf("BackgroundPID = 0; want non-zero")
	}
}

// TestRunStart_NoFromFile_PicksTask drives the re-plan branch:
// pre-seed a task in bbolt; UI.SelectSource returns SourceTask;
// UI.PickReplanTask returns the existing task's ID. RunStart must
// NOT mint a new task and must update the existing row's
// BackgroundPID + AgentLogPath in place.
func TestRunStart_NoFromFile_PicksTask(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	existingID := store.NewTaskID()
	if _, err := store.EnsureTaskDir(existingID); err != nil {
		t.Fatal(err)
	}
	seedTaskRowDirect(t, store.Task{
		ID:          existingID,
		Status:      store.StatusPlanDone,
		InvokedTool: "cursor",
		Summary:     "existing task",
	})
	ui := &scriptedStartUI{
		source:       picker.SourceTask,
		pickedTaskID: existingID,
		pickedTaskOK: true,
	}

	err := RunStart(context.Background(), StartOptions{
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: &scriptedSelector{},
		UI:       ui,
		JBinary:  noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if ui.taskCalls != 1 {
		t.Fatalf("PickReplanTask calls = %d, want 1", ui.taskCalls)
	}
	rows := allTaskRows(t)
	if len(rows) != 1 {
		t.Fatalf("ListTasks = %d rows, want exactly 1 (re-plan must reuse the existing task)", len(rows))
	}
	if rows[0].ID != existingID {
		t.Fatalf("row id = %q, want %q (no new task should have been minted)", rows[0].ID, existingID)
	}
	row := readTaskFromBolt(t, existingID)
	if row.BackgroundPID == 0 {
		t.Fatalf("existing row's BackgroundPID = 0; want non-zero PID stamped on re-plan")
	}
	if row.AgentLogPath == "" {
		t.Fatalf("existing row's AgentLogPath = %q; want non-empty", row.AgentLogPath)
	}
	if row.Status != store.StatusPlanDone {
		t.Fatalf("Status = %q; the orchestrator updates this asynchronously, the parent must leave it as-is", row.Status)
	}
	if row.Summary != "existing task" {
		t.Fatalf("Summary clobbered to %q; want %q", row.Summary, "existing task")
	}
}

// TestRunStart_NoFromFile_PicksLinear pins the linear no-op branch:
// SelectSource returns SourceLinear; RunStart prints a message and
// returns nil with no spawn fired and no row created.
func TestRunStart_NoFromFile_PicksLinear(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	ui := &scriptedStartUI{source: picker.SourceLinear}
	var stdout bytes.Buffer

	err := RunStart(context.Background(), StartOptions{
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: &scriptedSelector{},
		UI:       ui,
		JBinary:  noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if !strings.Contains(stdout.String(), "linear source is not yet wired up") {
		t.Fatalf("stdout = %q, want unwired-source message", stdout.String())
	}
	if rows := allTaskRows(t); len(rows) != 0 {
		t.Fatalf("ListTasks = %d, want 0 (linear branch must not create a row)", len(rows))
	}
}

// (No empty-cwd test here: that branch lives inside
// picker.PickMarkdownInCwd and is exercised by
// internal/cli/picker/picker_test.go::TestPickMarkdownInCwd_NoFiles.)

// TestRunStart_NoFromFile_NoExistingTasks pins the empty-bbolt
// branch on the re-plan source: SourceTask + no rows → wrapped
// error from pickReplanTarget; no spawn.
func TestRunStart_NoFromFile_NoExistingTasks(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	ui := &scriptedStartUI{source: picker.SourceTask}

	err := RunStart(context.Background(), StartOptions{
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: &scriptedSelector{},
		UI:       ui,
		JBinary:  noopJBinary(t),
	})
	if err == nil || !strings.Contains(err.Error(), "no tasks to re-plan") {
		t.Fatalf("err = %v, want no-tasks-to-replan wrap", err)
	}
}

// TestRunStart_NoFromFile_TaskPickerCancelled pins the picker-abort
// path on the re-plan source: PickReplanTask returns ok=false →
// RunStart exits cleanly with no spawn.
func TestRunStart_NoFromFile_TaskPickerCancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	existingID := store.NewTaskID()
	if _, err := store.EnsureTaskDir(existingID); err != nil {
		t.Fatal(err)
	}
	seedTaskRowDirect(t, store.Task{
		ID:          existingID,
		Status:      store.StatusPlanDone,
		InvokedTool: "cursor",
		Summary:     "existing",
	})
	ui := &scriptedStartUI{source: picker.SourceTask, pickedTaskOK: false}

	err := RunStart(context.Background(), StartOptions{
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: &scriptedSelector{},
		UI:       ui,
		JBinary:  noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("err = %v, want nil (cancelled picker exits cleanly)", err)
	}
	row := readTaskFromBolt(t, existingID)
	if row.BackgroundPID != 0 {
		t.Fatalf("existing row's BackgroundPID = %d, want 0 (picker cancel must not fire spawn)", row.BackgroundPID)
	}
}

// TestRunStart_SelectorAbortIsClean pins the deferred huh.ErrUserAborted
// guard for the agent-pick prompt.
func TestRunStart_SelectorAbortIsClean(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeStartFile(t, "# task\nbody")
	sel := &scriptedSelector{toolErr: huh.ErrUserAborted}
	if err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: sel,
		UI:       &scriptedStartUI{},
		JBinary:  noopJBinary(t),
	}); err != nil {
		t.Fatalf("err = %v, want nil (abort exits cleanly)", err)
	}
	if rows := allTaskRows(t); len(rows) != 0 {
		t.Fatalf("ListTasks = %d, want 0 after abort", len(rows))
	}
}

// TestRunStart_ResolveSourceFails pins the read-source error branch:
// pointing --from-file at a non-existent path surfaces a wrapped
// error before any row is seeded.
func TestRunStart_ResolveSourceFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	err := RunStart(context.Background(), StartOptions{
		FromFile: "/definitely/does/not/exist.md",
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: &scriptedSelector{},
		UI:       &scriptedStartUI{},
		JBinary:  noopJBinary(t),
	})
	if err == nil {
		t.Fatal("expected error from missing --from-file path")
	}
}

// TestRunStart_SpawnFails pins the SpawnIn error branch.
func TestRunStart_SpawnFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: &scriptedSelector{},
		UI:       &scriptedStartUI{},
		JBinary:  "/no/such/binary-xyzzy",
	})
	if err == nil {
		t.Fatal("expected spawn failure")
	}
}

// TestRunStart_AppliesDefaults exercises StartOptions.withDefaults
// (the nil-stdin / nil-stdout / nil-stderr / nil-Selector / nil-UI
// branches) by running with populated buckets so the selector + UI
// are never invoked.
func TestRunStart_AppliesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	if err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		JBinary:  noopJBinary(t),
	}); err != nil {
		t.Fatalf("RunStart: %v", err)
	}
}

// TestRunStart_BucketInteractiveUntouched pins one of the plan's
// acceptance criteria: the planner / worker / verifier buckets'
// stored `interactive` flag must be unchanged before vs. after
// `j tasks start`.
func TestRunStart_BucketInteractiveUntouched(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
		path, err := store.DefaultPath()
		if err != nil {
			t.Fatal(err)
		}
		s, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Put(bucket, "interactive", "true"); err != nil {
			t.Fatal(err)
		}
		_ = s.Close()
	}
	target := writeStartFile(t, "# task\nbody")
	if err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: &scriptedSelector{},
		UI:       &scriptedStartUI{},
		JBinary:  noopJBinary(t),
	}); err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		_, _, interactive := readAgentBucket(t, bucket)
		if interactive != "true" {
			t.Fatalf("bucket %q interactive = %q, want unchanged \"true\"", bucket, interactive)
		}
	}
}

// TestNewStartCmd_FlagDefaults pins the registered flag set.
func TestNewStartCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newStartCmd()
	if cmd.Use != "start" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	if len(names) != 1 || names[0] != "from-file" {
		t.Fatalf("flags = %v, want only [from-file]", names)
	}
	if cmd.Flags().Lookup("interactive") != nil {
		t.Fatal("--interactive should not be registered on `j tasks start`")
	}
}

// TestNewStartCmd_FlagsBindToViper covers --from-file piping
// through viper.
func TestNewStartCmd_FlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newStartCmd()
	if err := cmd.Flags().Set("from-file", "/tmp/foo.md"); err != nil {
		t.Fatalf("Flags().Set from-file: %v", err)
	}
	if got := viper.GetString("tasks.start.from_file"); got != "/tmp/foo.md" {
		t.Errorf("tasks.start.from_file = %q", got)
	}
}

// TestNewStartCmd_EnvBindings covers TASKS_START_FROM_FILE.
func TestNewStartCmd_EnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_START_FROM_FILE", "/env/foo.md")
	_ = newStartCmd()
	if got := viper.GetString("tasks.start.from_file"); got != "/env/foo.md" {
		t.Errorf("tasks.start.from_file = %q", got)
	}
}

// TestNewStartCmd_RunE_PropagatesError exercises the RunE closure
// end to end with a nonexistent --from-file path.
func TestNewStartCmd_RunE_PropagatesError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	cmd := newStartCmd()
	if err := cmd.Flags().Set("from-file", "/does/not/exist.md"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("expected an error from missing --from-file path")
	}
}

// TestRunStart_RegisteredAsChild verifies `j tasks start` is wired
// as a cobra child of `j tasks`.
func TestRunStart_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "start" {
			return
		}
	}
	t.Fatal("`j tasks start` should be registered as a child of `j tasks`")
}

// TestResolveJBinary_Default exercises the os.Executable fallback.
func TestResolveJBinary_Default(t *testing.T) {
	got, err := resolveJBinary("")
	if err != nil {
		t.Fatalf("resolveJBinary: %v", err)
	}
	if got == "" {
		t.Fatalf("resolveJBinary(\"\") returned empty path")
	}
}

// TestResolveJBinary_Override exercises the explicit-override branch.
func TestResolveJBinary_Override(t *testing.T) {
	got, err := resolveJBinary("/explicit/j")
	if err != nil {
		t.Fatalf("resolveJBinary: %v", err)
	}
	if got != "/explicit/j" {
		t.Fatalf("resolveJBinary(/explicit/j) = %q", got)
	}
}

// TestRunStart_ContextCancellable pins ctx-cancellation propagation.
func TestRunStart_ContextCancellable(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunStart(ctx, StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: &scriptedSelector{},
		UI:       &scriptedStartUI{},
		JBinary:  noopJBinary(t),
	})
	if err == nil {
		_ = firstSeededTaskID(t)
		return
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v, want context-cancellation propagation", err)
	}
}

// seedTaskRowDirect inserts a Task row via the per-project tasks
// bbolt DB. Used by the re-plan tests to pre-seed an existing task
// without going through any phase lifecycle.
func seedTaskRowDirect(t *testing.T, row store.Task) {
	t.Helper()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(row); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
}
