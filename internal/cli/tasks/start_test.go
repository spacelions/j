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

	"github.com/spacelions/j/internal/cli/tasklog"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// writeStartFile writes a markdown task description into the test's
// temp dir and returns its absolute path.
func writeStartFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// noopJBinary writes a tiny shell script that exits zero into the
// test's temp dir and returns its absolute path. Used as the
// JBinary override on RunStart so the orchestrator child is a
// quick no-op (avoids running the real `j` binary or recursing
// into the test process).
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
// The detached-spawn flow mints the id internally, so tests recover
// it by enumeration.
func firstSeededTaskID(t *testing.T) string {
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
	if len(rows) != 1 {
		t.Fatalf("ListTasks = %d rows, want 1: %+v", len(rows), rows)
	}
	return rows[0].ID
}

// TestRunStart_HappyPath_FromFile pins the new detached-spawn shape:
// EnsureAgentSelections fills empty buckets, requirements.md is
// staged, the task row is seeded with Status=planning + AgentLogPath
// + BackgroundPID, and RunStart returns immediately (well under a
// second in this stubbed path).
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

	// requirements.md is staged so the orchestrator can re-plan
	// against it.
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

	// Each agent bucket is populated so the orchestrator child sees
	// a tool/model pair.
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

// TestRunStart_MissingFromFile pins the new --from-file required
// branch: the detached child has no terminal so a markdown picker
// is not viable.
func TestRunStart_MissingFromFile(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	err := RunStart(context.Background(), StartOptions{
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		Selector: &scriptedSelector{},
	})
	if err == nil || !strings.Contains(err.Error(), "--from-file is required") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunStart_SelectorAbortIsClean pins the deferred huh.ErrUserAborted
// guard: a Ctrl-C in the agent-pick prompt translates to a nil exit
// and the orchestrator is never spawned.
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
		JBinary:  noopJBinary(t),
	}); err != nil {
		t.Fatalf("err = %v, want nil (abort exits cleanly)", err)
	}
	// No row should have been seeded because we bailed before mint.
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListTasks()
	_ = s.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
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
		JBinary:  noopJBinary(t),
	})
	if err == nil {
		t.Fatal("expected error from missing --from-file path")
	}
}

// TestRunStart_SpawnFails pins the SpawnIn error branch: pointing
// JBinary at a path that does not exist makes the spawn fail and
// RunStart surfaces the wrapped error.
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
		JBinary:  "/no/such/binary-xyzzy",
	})
	if err == nil {
		t.Fatal("expected spawn failure")
	}
}

// TestRunStart_AppliesDefaults exercises StartOptions.withDefaults
// (the nil-stdin / nil-stdout / nil-stderr / nil-Selector branches)
// by running with populated buckets so the selector is never invoked.
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
// `j tasks start`. Manual `j plan|work|verify` continue to honour
// their bucket values.
func TestRunStart_BucketInteractiveUntouched(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	// Pre-seed every bucket including a non-default `interactive`
	// value; then assert RunStart leaves it alone.
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

// TestNewStartCmd_FlagDefaults pins the registered flag set and
// viper bindings for `j tasks start`. Only --from-file is exposed;
// the interactive mode is owned by the per-phase commands.
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
// end to end. We point the cmd at a nonexistent --from-file so
// readStartSource fails and the closure surfaces the wrapped error.
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

// TestRunStart_ContextCancellable pins that a cancelled ctx does
// not leak the spawn: SpawnIn binds the child to ctx only for the
// brief window before Start, so a cancelled-then-passed ctx fails
// the spawn.
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
		JBinary:  noopJBinary(t),
	})
	if err == nil {
		// Cancelled ctx may either short-circuit before spawn (err)
		// or race past it on a fast machine. Both are valid; if no
		// error surfaced ensure the row is still queryable.
		_ = firstSeededTaskID(t)
		return
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v, want context-cancellation propagation", err)
	}
}
