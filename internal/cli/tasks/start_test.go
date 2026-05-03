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

	"github.com/charmbracelet/huh"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// startScriptedAgent reuses scriptedAgent from agentcheck_test.go but
// makes Plan write the expected requirements.md / plan.md side effects
// so RunStart's downstream plan.Run lands the same artifacts a real
// agent would. The fake records its last PlanRequest so tests can
// assert what plan.Run forwarded.
type startScriptedAgent struct {
	scriptedAgent
	planned int
	lastReq codingagents.PlanRequest
}

func newStartAgent() *startScriptedAgent {
	return &startScriptedAgent{scriptedAgent: scriptedAgent{
		name:   "cursor",
		models: []string{"sonnet-4", "gpt-5"},
	}}
}

func (a *startScriptedAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	a.planned++
	a.lastReq = req
	body, err := os.ReadFile(req.FromFilePath)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(req.RequirementsOutputPath, body, 0o644); err != nil {
		return 0, err
	}
	if err := os.WriteFile(req.PlanOutputPath, []byte("1. step\n"), 0o644); err != nil {
		return 0, err
	}
	return 0, nil
}

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

// TestRunStart_HappyPath_FromFile pins AC#A1: a fresh project with no
// agent buckets, --from-file pointing at a markdown spec. RunStart
// must prompt three times (planner+worker+verifier), then plan.Run
// produces requirements.md + plan.md inside .j/tasks/<id>/.
func TestRunStart_HappyPath_FromFile(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeStartFile(t, "# task\nbody")
	agent := newStartAgent()
	sel := &scriptedSelector{tool: "cursor", model: "sonnet-4"}
	var stdout, stderr bytes.Buffer

	err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		Selector: sel,
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if sel.toolCalls != 3 || sel.modelCalls != 3 {
		t.Fatalf("selector calls = (%d, %d), want (3, 3)", sel.toolCalls, sel.modelCalls)
	}
	if agent.planned != 1 {
		t.Fatalf("agent.planned = %d, want 1", agent.planned)
	}
	if !strings.Contains(stdout.String(), ".j/tasks/") {
		t.Fatalf("stdout should mention task dir: %q", stdout.String())
	}
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		tool, model, _ := readAgentBucket(t, bucket)
		if tool != "cursor" || model != "sonnet-4" {
			t.Fatalf("bucket %q = (%q, %q)", bucket, tool, model)
		}
	}
}

// TestRunStart_PrePopulatedSkipsPrompts pins AC#A1's "only for any
// unconfigured agent buckets" half: with every bucket already
// populated, RunStart never invokes the selector and goes straight to
// plan.Run.
func TestRunStart_PrePopulatedSkipsPrompts(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	agent := newStartAgent()
	sel := &scriptedSelector{}

	err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		Selector: sel,
	})
	if err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if sel.toolCalls != 0 || sel.modelCalls != 0 {
		t.Fatalf("selector calls = (%d, %d), want (0, 0) when buckets are populated", sel.toolCalls, sel.modelCalls)
	}
	if agent.planned != 1 {
		t.Fatalf("agent.planned = %d, want 1", agent.planned)
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

// TestRunStart_SelectorAbortIsClean pins the deferred huh.ErrUserAborted
// guard: a Ctrl-C in the agent-pick prompt translates to a nil exit
// and plan.Run is never invoked.
func TestRunStart_SelectorAbortIsClean(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeStartFile(t, "# task\nbody")
	agent := newStartAgent()
	sel := &scriptedSelector{toolErr: huh.ErrUserAborted}
	if err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		Selector: sel,
	}); err != nil {
		t.Fatalf("err = %v, want nil (abort exits cleanly)", err)
	}
	if agent.planned != 0 {
		t.Fatalf("agent.planned = %d, want 0 after abort", agent.planned)
	}
}

// TestRunStart_AppliesDefaults exercises StartOptions.withDefaults
// (the nil-stdin / nil-stdout / nil-stderr / nil-Selector branches)
// by running with a populated bucket so the prompt path never fires
// (and thus stdin / Selector are unread).
func TestRunStart_AppliesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	agent := newStartAgent()
	if err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Agents:   []codingagents.Agent{agent},
	}); err != nil {
		t.Fatalf("RunStart: %v", err)
	}
}

// TestRunStart_InteractiveFromBucket pins that the planner bucket's
// stored `interactive` value flows through to agent.Plan. Resume has
// no --interactive flag; the stored value is authoritative.
func TestRunStart_InteractiveFromBucket(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	// Override the planner bucket's interactive=false so plan.Run
	// reads it.
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketPlanner, "interactive", "false"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	target := writeStartFile(t, "# task\nbody")
	agent := newStartAgent()
	if err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		Selector: &scriptedSelector{},
	}); err != nil {
		t.Fatalf("RunStart: %v", err)
	}
	if agent.lastReq.Interactive {
		t.Fatalf("Interactive forwarded to agent = true, want false (stored=false)")
	}
}

// TestNewStartCmd_FlagDefaults pins the registered flag set, defaults,
// and viper bindings for `j tasks start`. The flag surface is
// intentionally minimal: only --from-file. The interactive mode is
// read from the planner bucket so users do not re-supply it on
// every run.
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

// TestNewStartCmd_FlagsBindToViper covers --from-file piping through viper.
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

// TestNewStartCmd_RunE_PropagatesError exercises the RunE closure end
// to end. We point the cmd at a nonexistent --from-file so plan.Run
// returns an error and the closure surfaces it. The error proves the
// closure constructed StartOptions and reached RunStart.
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

// TestRunStart_RegisteredAsChild verifies `j tasks start` is wired as
// a cobra child of `j tasks`.
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

// TestRunStart_PropagatesPlanError pins that errors from the
// downstream planner surface verbatim to the caller.
func TestRunStart_PropagatesPlanError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
	target := writeStartFile(t, "# task\nbody")
	agent := newStartAgent()
	agent.scriptedAgent.loginErr = errors.New("login boom")
	err := RunStart(context.Background(), StartOptions{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		Selector: &scriptedSelector{},
	})
	if err == nil || !strings.Contains(err.Error(), "login boom") {
		t.Fatalf("err = %v", err)
	}
}
