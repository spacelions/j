package tasks

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/plan"
	"github.com/spacelions/j/internal/cli/verify"
	"github.com/spacelions/j/internal/cli/work"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
)

// redoCalls records every dispatch from runRedo so each test can
// assert resume vs re-run plus the forwarded TaskID / Tool / Model /
// Yes / Interactive values without invoking real agents.
type redoCalls struct {
	planRun        plan.Options
	planRunCount   int
	planResume     plan.ResumeOptions
	planResumeCnt  int
	workRun        work.Options
	workRunCount   int
	workResume     work.ResumeOptions
	workResumeCnt  int
	verifyRun      verify.Options
	verifyRunCount int
	verifyResume   verify.ResumeOptions
	verifyResCnt   int
}

// installRedoStubs replaces the dispatch surface with thin recorders
// so tests can pin which underlying entry point fired and what
// values were forwarded. The original functions are restored via
// t.Cleanup so parallel tests in the same binary cannot leak state.
//
// EnsureAgentSelections is also stubbed: every test that exercises
// runRedo would otherwise need to pre-seed every bucket; the stub
// short-circuits so the table-driven tests stay focused on the
// dispatch decision, with a separate test still covering the real
// EnsureAgentSelections wiring.
func installRedoStubs(t *testing.T) *redoCalls {
	t.Helper()
	calls := &redoCalls{}
	prevPlanRun := runRedoPlanRun
	prevPlanResume := runRedoPlanRunResume
	prevWorkRun := runRedoWorkRun
	prevWorkResume := runRedoWorkRunResume
	prevVerifyRun := runRedoVerifyRun
	prevVerifyResume := runRedoVerifyRunResume
	prevEnsure := runRedoEnsureSelections
	runRedoPlanRun = func(_ context.Context, opts plan.Options) error {
		calls.planRunCount++
		calls.planRun = opts
		return nil
	}
	runRedoPlanRunResume = func(_ context.Context, opts plan.ResumeOptions) error {
		calls.planResumeCnt++
		calls.planResume = opts
		return nil
	}
	runRedoWorkRun = func(_ context.Context, opts work.Options) error {
		calls.workRunCount++
		calls.workRun = opts
		return nil
	}
	runRedoWorkRunResume = func(_ context.Context, opts work.ResumeOptions) error {
		calls.workResumeCnt++
		calls.workResume = opts
		return nil
	}
	runRedoVerifyRun = func(_ context.Context, opts verify.Options) error {
		calls.verifyRunCount++
		calls.verifyRun = opts
		return nil
	}
	runRedoVerifyRunResume = func(_ context.Context, opts verify.ResumeOptions) error {
		calls.verifyResCnt++
		calls.verifyResume = opts
		return nil
	}
	runRedoEnsureSelections = func(context.Context, AgentCheckOptions) error { return nil }
	t.Cleanup(func() {
		runRedoPlanRun = prevPlanRun
		runRedoPlanRunResume = prevPlanResume
		runRedoWorkRun = prevWorkRun
		runRedoWorkRunResume = prevWorkResume
		runRedoVerifyRun = prevVerifyRun
		runRedoVerifyRunResume = prevVerifyResume
		runRedoEnsureSelections = prevEnsure
	})
	return calls
}

func setupRedoEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	mustInit(t)
}

// seedRedoTask seeds a single bbolt task row, applying the mutate
// hook so each test customises status / cursor / per-phase
// tool/model verbatim.
func seedRedoTask(t *testing.T, mutate func(*store.Task)) string {
	t.Helper()
	id := store.NewTaskID()
	if _, err := store.EnsureTaskDir(id); err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	task := store.Task{
		ID:           id,
		Status:       store.StatusPlanDone,
		InvokedTool:  "cursor",
		InvokedModel: "sonnet-4",
		Summary:      "seed",
	}
	if mutate != nil {
		mutate(&task)
	}
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestRunRedo_ResumeDispatch covers the resume branch for every
// phase: a non-empty resume cursor routes through the matching
// RunResume helper with TaskID set.
func TestRunRedo_ResumeDispatch(t *testing.T) {
	cases := []struct {
		name  string
		phase redoPhase
		mut   func(*store.Task)
		check func(*testing.T, *redoCalls, string)
	}{
		{
			name:  "plan resume",
			phase: redoPhasePlan,
			mut:   func(task *store.Task) { task.PlanResumeCursor = "p-cursor" },
			check: func(t *testing.T, calls *redoCalls, id string) {
				if calls.planResumeCnt != 1 || calls.planRunCount != 0 {
					t.Fatalf("plan dispatch counts = (resume=%d, run=%d), want (1, 0)", calls.planResumeCnt, calls.planRunCount)
				}
				if calls.planResume.TaskID != id {
					t.Fatalf("plan resume TaskID = %q, want %q", calls.planResume.TaskID, id)
				}
			},
		},
		{
			name:  "work resume",
			phase: redoPhaseWork,
			mut:   func(task *store.Task) { task.WorkResumeCursor = "w-cursor" },
			check: func(t *testing.T, calls *redoCalls, id string) {
				if calls.workResumeCnt != 1 || calls.workRunCount != 0 {
					t.Fatalf("work dispatch counts = (resume=%d, run=%d), want (1, 0)", calls.workResumeCnt, calls.workRunCount)
				}
				if calls.workResume.TaskID != id {
					t.Fatalf("work resume TaskID = %q, want %q", calls.workResume.TaskID, id)
				}
			},
		},
		{
			name:  "verify resume",
			phase: redoPhaseVerify,
			mut:   func(task *store.Task) { task.VerifyResumeCursor = "v-cursor" },
			check: func(t *testing.T, calls *redoCalls, id string) {
				if calls.verifyResCnt != 1 || calls.verifyRunCount != 0 {
					t.Fatalf("verify dispatch counts = (resume=%d, run=%d), want (1, 0)", calls.verifyResCnt, calls.verifyRunCount)
				}
				if calls.verifyResume.TaskID != id {
					t.Fatalf("verify resume TaskID = %q, want %q", calls.verifyResume.TaskID, id)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupRedoEnv(t)
			calls := installRedoStubs(t)
			id := seedRedoTask(t, tc.mut)
			err := runRedo(context.Background(), tc.phase, RedoOptions{
				TaskID:      id,
				Interactive: true,
				Stdin:       strings.NewReader(""),
				Stdout:      io.Discard,
				Stderr:      io.Discard,
				Agents:      []codingagents.Agent{newScriptedAgent()},
				UI:          &fakeUI{},
			})
			if err != nil {
				t.Fatalf("runRedo: %v", err)
			}
			tc.check(t, calls, id)
		})
	}
}

// TestRunRedo_RerunUsesPerPhaseToolModel pins the re-run branch:
// empty resume cursor + populated per-phase tool/model -> the
// matching Run helper is called with Tool/Model from the row,
// Yes=true, Store=nil, and Interactive forwarded from the options.
func TestRunRedo_RerunUsesPerPhaseToolModel(t *testing.T) {
	cases := []struct {
		name  string
		phase redoPhase
		mut   func(*store.Task)
		check func(*testing.T, *redoCalls, string)
	}{
		{
			name:  "plan rerun",
			phase: redoPhasePlan,
			mut: func(task *store.Task) {
				task.PlanTool = "claude"
				task.PlanModel = "opus-4-7"
			},
			check: func(t *testing.T, calls *redoCalls, id string) {
				if calls.planRunCount != 1 {
					t.Fatalf("plan run count = %d, want 1", calls.planRunCount)
				}
				assertRerunPlanOptions(t, calls.planRun, id, "claude", "opus-4-7", true)
			},
		},
		{
			name:  "work rerun",
			phase: redoPhaseWork,
			mut: func(task *store.Task) {
				task.WorkTool = "cursor"
				task.WorkModel = "gpt-5"
			},
			check: func(t *testing.T, calls *redoCalls, id string) {
				if calls.workRunCount != 1 {
					t.Fatalf("work run count = %d, want 1", calls.workRunCount)
				}
				assertRerunWorkOptions(t, calls.workRun, id, "cursor", "gpt-5", true)
			},
		},
		{
			name:  "verify rerun",
			phase: redoPhaseVerify,
			mut: func(task *store.Task) {
				task.VerifyTool = "cursor"
				task.VerifyModel = "sonnet-4"
			},
			check: func(t *testing.T, calls *redoCalls, id string) {
				if calls.verifyRunCount != 1 {
					t.Fatalf("verify run count = %d, want 1", calls.verifyRunCount)
				}
				assertRerunVerifyOptions(t, calls.verifyRun, id, "cursor", "sonnet-4", true)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupRedoEnv(t)
			calls := installRedoStubs(t)
			id := seedRedoTask(t, tc.mut)
			err := runRedo(context.Background(), tc.phase, RedoOptions{
				TaskID:      id,
				Interactive: true,
				Stdin:       strings.NewReader(""),
				Stdout:      io.Discard,
				Stderr:      io.Discard,
				Agents:      []codingagents.Agent{newScriptedAgent()},
				UI:          &fakeUI{},
			})
			if err != nil {
				t.Fatalf("runRedo: %v", err)
			}
			tc.check(t, calls, id)
		})
	}
}

// TestRunRedo_RerunEmptyPerPhase pins the bucket-fallback branch:
// empty resume cursor + empty per-phase tool/model -> Tool/Model
// stay empty so resolver.Agent transparently reads the bucket.
func TestRunRedo_RerunEmptyPerPhase(t *testing.T) {
	setupRedoEnv(t)
	calls := installRedoStubs(t)
	id := seedRedoTask(t, nil)
	err := runRedo(context.Background(), redoPhasePlan, RedoOptions{
		TaskID:      id,
		Interactive: true,
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newScriptedAgent()},
		UI:          &fakeUI{},
	})
	if err != nil {
		t.Fatalf("runRedo: %v", err)
	}
	if calls.planRunCount != 1 {
		t.Fatalf("plan run count = %d, want 1", calls.planRunCount)
	}
	if calls.planRun.Tool != "" || calls.planRun.Model != "" {
		t.Fatalf("Tool/Model = (%q, %q), want both empty so resolver.Agent falls back to the bucket",
			calls.planRun.Tool, calls.planRun.Model)
	}
}

// TestRunRedo_FromTaskSkipsPicker confirms that a non-empty TaskID
// short-circuits the picker (UI.PickTask is never called).
func TestRunRedo_FromTaskSkipsPicker(t *testing.T) {
	setupRedoEnv(t)
	installRedoStubs(t)
	id := seedRedoTask(t, nil)
	ui := &fakeUI{}
	err := runRedo(context.Background(), redoPhasePlan, RedoOptions{
		TaskID:      id,
		Interactive: true,
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newScriptedAgent()},
		UI:          ui,
	})
	if err != nil {
		t.Fatalf("runRedo: %v", err)
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask calls = %d, want 0 (--from-task should skip the picker)", ui.pickCalls)
	}
}

// TestRunRedo_PickerHappy pins the no-flag picker path: the user
// selects one row and dispatch fires for it.
func TestRunRedo_PickerHappy(t *testing.T) {
	setupRedoEnv(t)
	calls := installRedoStubs(t)
	id := seedRedoTask(t, func(task *store.Task) { task.PlanResumeCursor = "p" })
	ui := &fakeUI{pickReturn: id}
	if err := runRedo(context.Background(), redoPhasePlan, RedoOptions{
		Interactive: true,
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newScriptedAgent()},
		UI:          ui,
	}); err != nil {
		t.Fatalf("runRedo: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("pickCalls = %d, want 1", ui.pickCalls)
	}
	if calls.planResumeCnt != 1 {
		t.Fatalf("planResume = %d, want 1", calls.planResumeCnt)
	}
}

// TestRunRedo_InteractiveFalse pins the explicit override on the
// re-run branch: --interactive=false flows into Options.Interactive.
func TestRunRedo_InteractiveFalse(t *testing.T) {
	setupRedoEnv(t)
	calls := installRedoStubs(t)
	id := seedRedoTask(t, func(task *store.Task) {
		task.PlanTool = "cursor"
		task.PlanModel = "sonnet-4"
	})
	if err := runRedo(context.Background(), redoPhasePlan, RedoOptions{
		TaskID:      id,
		Interactive: false,
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newScriptedAgent()},
		UI:          &fakeUI{},
	}); err != nil {
		t.Fatalf("runRedo: %v", err)
	}
	if calls.planRun.Interactive {
		t.Fatalf("plan run Interactive = true, want false")
	}
}

// TestRunRedo_NoAgents pins the no-agents-configured branch.
func TestRunRedo_NoAgents(t *testing.T) {
	err := runRedo(context.Background(), redoPhasePlan, RedoOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunRedo_PickerCancel pins user-cancel from the picker:
// returns nil with no dispatch.
func TestRunRedo_PickerCancel(t *testing.T) {
	setupRedoEnv(t)
	calls := installRedoStubs(t)
	seedRedoTask(t, nil)
	seedRedoTask(t, nil)
	if err := runRedo(context.Background(), redoPhasePlan, RedoOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     &fakeUI{},
	}); err != nil {
		t.Fatalf("runRedo: %v", err)
	}
	if calls.planRunCount+calls.planResumeCnt != 0 {
		t.Fatalf("no dispatch should fire on cancel; got run=%d resume=%d", calls.planRunCount, calls.planResumeCnt)
	}
}

// TestRunRedo_AgentSelectorAborts pins the deferred huh.ErrUserAborted
// guard: a Ctrl-C in EnsureAgentSelections exits cleanly with no
// dispatch. This test exercises the real EnsureAgentSelections path
// (no stub) so we leave runRedoEnsureSelections at its production
// value.
func TestRunRedo_AgentSelectorAborts(t *testing.T) {
	setupRedoEnv(t)
	// Stub the dispatch surface only; leave the real
	// EnsureAgentSelections in place so the abort path fires.
	prevEnsure := runRedoEnsureSelections
	calls := installRedoStubs(t)
	runRedoEnsureSelections = prevEnsure
	t.Cleanup(func() { runRedoEnsureSelections = prevEnsure })
	id := seedRedoTask(t, nil) // no agent buckets seeded -> selector prompts
	if err := runRedo(context.Background(), redoPhasePlan, RedoOptions{
		TaskID:   id,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		UI:       &fakeUI{},
		Selector: &testutil.SelectorFake{ToolErr: huh.ErrUserAborted},
	}); err != nil {
		t.Fatalf("runRedo: %v, want nil (abort exits cleanly)", err)
	}
	if calls.planRunCount+calls.planResumeCnt != 0 {
		t.Fatal("no dispatch should fire after abort")
	}
}

// TestRunRedo_NoTasksFile pins the missing-tasks-db short-circuit:
// no list.db, no --from-task -> emptyMessage and exit 0 with no
// dispatch.
func TestRunRedo_NoTasksFile(t *testing.T) {
	t.Chdir(t.TempDir())
	calls := installRedoStubs(t)
	if err := runRedo(context.Background(), redoPhasePlan, RedoOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     &fakeUI{},
	}); err != nil {
		t.Fatalf("runRedo: %v", err)
	}
	if calls.planRunCount+calls.planResumeCnt != 0 {
		t.Fatal("no dispatch should fire when list.db is missing")
	}
}

// TestRunRedo_DispatchError exercises the error-propagation path:
// the stubbed Run returns an error and runRedo surfaces it verbatim.
func TestRunRedo_DispatchError(t *testing.T) {
	setupRedoEnv(t)
	installRedoStubs(t)
	runRedoPlanRun = func(_ context.Context, _ plan.Options) error {
		return errors.New("plan boom")
	}
	id := seedRedoTask(t, func(task *store.Task) {
		task.PlanTool = "cursor"
		task.PlanModel = "sonnet-4"
	})
	err := runRedo(context.Background(), redoPhasePlan, RedoOptions{
		TaskID:      id,
		Interactive: true,
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newScriptedAgent()},
		UI:          &fakeUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "plan boom") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunRedo_UnsupportedPhase covers the safety-net branch in
// dispatchRedo when an unknown phase reaches the switch.
func TestRunRedo_UnsupportedPhase(t *testing.T) {
	err := dispatchRedo(context.Background(), redoPhase("ghost"), RedoOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
	}, store.Task{ID: "x"})
	if err == nil || !strings.Contains(err.Error(), "unsupported redo phase") {
		t.Fatalf("err = %v", err)
	}
}

// TestRedoOptions_AppliesDefaults exercises RedoOptions.withDefaults.
func TestRedoOptions_AppliesDefaults(t *testing.T) {
	setupRedoEnv(t)
	installRedoStubs(t)
	if err := runRedo(context.Background(), redoPhasePlan, RedoOptions{
		Agents: []codingagents.Agent{newScriptedAgent()},
	}); err != nil {
		t.Fatalf("runRedo: %v", err)
	}
}

// assertRerunPlanOptions / assertRerunWorkOptions /
// assertRerunVerifyOptions encapsulate the shared shape for the
// re-run dispatch: TaskID, Tool, Model, Yes=true, plus a per-call
// Interactive value.
func assertRerunPlanOptions(t *testing.T, opts plan.Options, id, tool, model string, interactive bool) {
	t.Helper()
	if opts.TaskID != id {
		t.Fatalf("TaskID = %q, want %q", opts.TaskID, id)
	}
	if opts.Tool != tool || opts.Model != model {
		t.Fatalf("Tool/Model = (%q, %q), want (%q, %q)", opts.Tool, opts.Model, tool, model)
	}
	if !opts.Yes {
		t.Fatalf("Yes = false, want true (explicit re-run skips the status confirm)")
	}
	if opts.Interactive != interactive {
		t.Fatalf("Interactive = %v, want %v", opts.Interactive, interactive)
	}
	if opts.Store != nil {
		t.Fatalf("Store = %v, want nil (re-run never persists tool/model into the bucket)", opts.Store)
	}
}

func assertRerunWorkOptions(t *testing.T, opts work.Options, id, tool, model string, interactive bool) {
	t.Helper()
	if opts.TaskID != id {
		t.Fatalf("TaskID = %q, want %q", opts.TaskID, id)
	}
	if opts.Tool != tool || opts.Model != model {
		t.Fatalf("Tool/Model = (%q, %q), want (%q, %q)", opts.Tool, opts.Model, tool, model)
	}
	if !opts.Yes {
		t.Fatalf("Yes = false, want true")
	}
	if opts.Interactive != interactive {
		t.Fatalf("Interactive = %v, want %v", opts.Interactive, interactive)
	}
	if opts.Store != nil {
		t.Fatalf("Store = %v, want nil", opts.Store)
	}
}

func assertRerunVerifyOptions(t *testing.T, opts verify.Options, id, tool, model string, interactive bool) {
	t.Helper()
	if opts.TaskID != id {
		t.Fatalf("TaskID = %q, want %q", opts.TaskID, id)
	}
	if opts.Tool != tool || opts.Model != model {
		t.Fatalf("Tool/Model = (%q, %q), want (%q, %q)", opts.Tool, opts.Model, tool, model)
	}
	if !opts.Yes {
		t.Fatalf("Yes = false, want true")
	}
	if opts.Interactive != interactive {
		t.Fatalf("Interactive = %v, want %v", opts.Interactive, interactive)
	}
	if opts.Store != nil {
		t.Fatalf("Store = %v, want nil", opts.Store)
	}
}
