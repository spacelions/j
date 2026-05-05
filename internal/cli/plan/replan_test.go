package plan

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

	"github.com/spacelions/j/internal/cli/picker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
)

// taskFilePath returns the expected absolute location of `<id>/<name>`
// under the per-cwd tasks directory.
func taskFilePath(t *testing.T, id, name string) string {
	t.Helper()
	dir, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	return filepath.Join(dir, id, name)
}

// seedReplanTask writes a task row with the supplied status,
// requirements body, and (optionally) a fresh PlanBeginAt. Used by
// the re-plan tests to drive runReplanTask against a known state.
func seedReplanTask(t *testing.T, status store.TaskStatus, requirement string, planBegin *time.Time) string {
	t.Helper()
	id := store.NewTaskID()
	if _, err := store.EnsureTaskDir(id); err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if requirement != "" {
		reqPath := taskFilePath(t, id, store.RequirementsFileName)
		if err := os.WriteFile(reqPath, []byte(requirement), 0o644); err != nil {
			t.Fatalf("write requirements: %v", err)
		}
	}
	dbPath, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	s := store.OpenTasks(dbPath)
	defer func() { _ = s.Close() }()
	task := store.Task{
		ID:               id,
		Status:           status,
		InvokedTool:      "cursor-prev",
		InvokedModel:     "opus-prev",
		PlanResumeCursor: "seed-resume",
		Summary:          "seed summary",
		PlanBeginAt:      planBegin,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}

// TestAllowedForReplan covers every status branch in the re-plan
// allowlist: only plan-done and help return true; everything else
// (planning / working / verifying / done / unknown) routes to the
// confirm prompt.
func TestAllowedForReplan(t *testing.T) {
	cases := []struct {
		status store.TaskStatus
		want   bool
	}{
		{store.StatusPlanDone, true},
		{store.StatusHelp, true},
		{store.StatusPlanning, false},
		{store.StatusWorking, false},
		{store.StatusVerifying, false},
		{store.StatusCompleted, false},
		{store.StatusWorkDone, false},
		{store.StatusVerifyDone, false},
		{store.TaskStatus("unknown"), false},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			got := resolver.ReplanAllowed(store.Task{Status: tc.status})
			if got != tc.want {
				t.Fatalf("allowedForReplan(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// TestConfirmStatusOverride_AllowedShortCircuits covers the
// allowlist short-circuit: an allowed status returns proceed=true
// without invoking the UI.
func TestConfirmStatusOverride_AllowedShortCircuits(t *testing.T) {
	ui := &scriptedUI{}
	proceed, err := resolver.ConfirmStatusOverride(context.Background(), ui, false,
		"re-plan",
		store.Task{ID: "id1", Status: store.StatusPlanDone},
		resolver.ReplanAllowed)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !proceed {
		t.Fatal("proceed = false, want true (allowed status)")
	}
	if ui.confirmCalls != 0 {
		t.Fatalf("ConfirmStatusOverride calls = %d, want 0", ui.confirmCalls)
	}
}

// TestConfirmStatusOverride_YesFlagSkipsPrompt covers the --yes
// branch: a wrong status with Yes=true returns proceed=true and
// the UI is left untouched.
func TestConfirmStatusOverride_YesFlagSkipsPrompt(t *testing.T) {
	ui := &scriptedUI{}
	proceed, err := resolver.ConfirmStatusOverride(context.Background(),
		ui, true,
		"re-plan",
		store.Task{ID: "id1", Status: store.StatusWorking},
		resolver.ReplanAllowed)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !proceed {
		t.Fatal("proceed = false, want true (Yes=true)")
	}
	if ui.confirmCalls != 0 {
		t.Fatalf("ConfirmStatusOverride calls = %d, want 0", ui.confirmCalls)
	}
}

// TestConfirmStatusOverride_PromptYes covers the prompted-and-accepted
// branch: a wrong status with the UI returning true yields
// proceed=true.
func TestConfirmStatusOverride_PromptYes(t *testing.T) {
	ui := &scriptedUI{confirm: true}
	proceed, err := resolver.ConfirmStatusOverride(context.Background(),
		ui, false,
		"re-plan",
		store.Task{ID: "id1", Status: store.StatusWorking},
		resolver.ReplanAllowed)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !proceed {
		t.Fatal("proceed = false, want true (UI accepted)")
	}
	if ui.confirmCalls != 1 {
		t.Fatalf("ConfirmStatusOverride calls = %d, want 1", ui.confirmCalls)
	}
	if ui.confirmCmd != "re-plan" || ui.confirmStatus != string(store.StatusWorking) || ui.confirmTaskID != "id1" {
		t.Fatalf("confirm args = (%q, %q, %q)", ui.confirmCmd, ui.confirmTaskID, ui.confirmStatus)
	}
}

// TestConfirmStatusOverride_PromptNo covers the prompted-and-declined
// branch: a wrong status with the UI returning false yields
// proceed=false and a nil error.
func TestConfirmStatusOverride_PromptNo(t *testing.T) {
	ui := &scriptedUI{confirm: false}
	proceed, err := resolver.ConfirmStatusOverride(context.Background(),
		ui, false,
		"re-plan",
		store.Task{ID: "id1", Status: store.StatusWorking},
		resolver.ReplanAllowed)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if proceed {
		t.Fatal("proceed = true, want false (UI declined)")
	}
}

// TestConfirmStatusOverride_PromptError surfaces a UI error verbatim
// without consulting Yes (the prompt is the source of truth here).
func TestConfirmStatusOverride_PromptError(t *testing.T) {
	ui := &scriptedUI{confirmErr: errors.New("confirm boom")}
	proceed, err := resolver.ConfirmStatusOverride(context.Background(),
		ui, false,
		"re-plan",
		store.Task{ID: "id1", Status: store.StatusWorking},
		resolver.ReplanAllowed)
	if err == nil || !strings.Contains(err.Error(), "confirm boom") {
		t.Fatalf("err = %v", err)
	}
	if proceed {
		t.Fatal("proceed = true, want false on prompt error")
	}
}

// TestLoadTaskByID_NotFound confirms the io/fs.ErrNotExist wrap into
// the user-facing "task %q not found" error.
func TestLoadTaskByID_NotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	_, err := loadTaskByID("ghost")
	if err == nil || !strings.Contains(err.Error(), `task "ghost" not found`) {
		t.Fatalf("err = %v", err)
	}
}

// TestLoadTaskByID_Success returns the seeded row verbatim.
func TestLoadTaskByID_Success(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusPlanDone, "# req", nil)
	got, err := loadTaskByID(id)
	if err != nil {
		t.Fatalf("loadTaskByID: %v", err)
	}
	if got.ID != id || got.Status != store.StatusPlanDone {
		t.Fatalf("got = %+v", got)
	}
}

// TestListAllTasks_OpenFails covers the open-failure branch.
// TestListAllTasks_SortsAndReturns confirms the helper returns every
// row sorted by store.SortTasks (active-first ordering).
func TestListAllTasks_SortsAndReturns(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id1 := seedReplanTask(t, store.StatusPlanDone, "a", nil)
	id2 := seedReplanTask(t, store.StatusPlanning, "b", nil)
	got, err := listAllTasks()
	if err != nil {
		t.Fatalf("listAllTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// store.SortTasks puts active (planning/working/verifying)
	// rows first; the planning row therefore precedes plan-done.
	if got[0].ID != id2 || got[1].ID != id1 {
		t.Fatalf("order = [%s, %s], want [%s, %s]", got[0].ID, got[1].ID, id2, id1)
	}
}

// TestRun_FromTask_BypassesSourceSelector pins the rule that
// --from-task takes the re-plan path without prompting for a source.
func TestRun_FromTask_BypassesSourceSelector(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusPlanDone, "# req\nbody", nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.sourceCalls != 0 {
		t.Fatalf("SelectSource should not be invoked, got %d", ui.sourceCalls)
	}
	if agent.planned != 1 {
		t.Fatalf("agent.planned = %d, want 1", agent.planned)
	}
}

// TestRun_FromTask_NotFound surfaces the load error before any
// agent or UI interaction.
func TestRun_FromTask_NotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		TaskID: "ghost",
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), `task "ghost" not found`) {
		t.Fatalf("err = %v", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan must not run when the task is missing")
	}
}

// TestRun_FromTask_StatusMismatch_DeclinedExitsClean covers the
// re-plan prompt-on-mismatch contract: a `working` task triggers
// the confirm prompt; a `no` answer exits cleanly with nil and
// leaves the row untouched.
func TestRun_FromTask_StatusMismatch_DeclinedExitsClean(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusWorking, "# req", nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{confirm: false}
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ui.confirmCalls != 1 {
		t.Fatalf("ConfirmStatusOverride calls = %d, want 1", ui.confirmCalls)
	}
	if ui.confirmCmd != "re-plan" || ui.confirmTaskID != id || ui.confirmStatus != string(store.StatusWorking) {
		t.Fatalf("confirm args = (%q, %q, %q)", ui.confirmCmd, ui.confirmTaskID, ui.confirmStatus)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan should not run when the user declines")
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusWorking {
		t.Fatalf("declined task should stay working: %+v", tasks)
	}
}

// TestRun_FromTask_StatusMismatch_AcceptedRuns pins the accepted-prompt
// branch: confirm=true makes the re-plan run against a wrong-status
// task and the row flips to plan-done.
func TestRun_FromTask_StatusMismatch_AcceptedRuns(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusWorking, "# old req\nbody", nil)
	agent := newScriptedAgent()
	agent.requirement = "# refreshed req"
	agent.plan = "1. step\n2. step"
	ui := &scriptedUI{confirm: true}
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if agent.planned != 1 {
		t.Fatalf("agent.planned = %d, want 1", agent.planned)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusPlanDone {
		t.Fatalf("tasks = %+v, want one plan-done row", tasks)
	}
}

// TestRun_FromTask_YesFlagSkipsPrompt covers the --yes path on the
// re-plan flow: a wrong-status task with Yes=true skips the prompt
// and runs the agent.
func TestRun_FromTask_YesFlagSkipsPrompt(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusVerifying, "# req\nbody", nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	err := Run(context.Background(), Options{
		TaskID: id,
		Yes:    true,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.confirmCalls != 0 {
		t.Fatalf("ConfirmStatusOverride calls = %d, want 0 with Yes=true", ui.confirmCalls)
	}
	if agent.planned != 1 {
		t.Fatalf("agent.planned = %d, want 1", agent.planned)
	}
}

// TestRun_FromTask_StatusMismatch_PromptError surfaces a UI error
// from ConfirmStatusOverride and skips the agent.
func TestRun_FromTask_StatusMismatch_PromptError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusPlanning, "# req", nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{confirmErr: errors.New("confirm boom")}
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "confirm boom") {
		t.Fatalf("err = %v", err)
	}
	if agent.planned != 0 {
		t.Fatal("planner must not run when the prompt errored")
	}
}

// TestRun_FromTask_StatusMismatch_AbortExitsClean pins the huh
// abort path through the re-plan confirm prompt: aborting yields
// a clean nil exit (consistent with the deferred guard in Run).
func TestRun_FromTask_StatusMismatch_AbortExitsClean(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusCompleted, "# req", nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{confirmErr: huh.ErrUserAborted}
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil (abort exits cleanly)", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent.Plan must not run after abort")
	}
}

// TestRun_FromTask_PreservesPlanBeginAt confirms beginPlanTaskReuse
// keeps the original PlanBeginAt while flipping status through
// planning → plan-done. The summary is regenerated from the refreshed
// requirements body so users see the latest content.
func TestRun_FromTask_PreservesPlanBeginAt(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	original := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := seedReplanTask(t, store.StatusPlanDone, "# original\nbody", &original)
	agent := newScriptedAgent()
	agent.requirement = "# refreshed\nbody"
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len = %d, want 1", len(tasks))
	}
	got := tasks[0]
	if got.Status != store.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.PlanBeginAt == nil || !got.PlanBeginAt.Equal(original) {
		t.Fatalf("PlanBeginAt = %v, want %v", got.PlanBeginAt, original)
	}
	if got.PlanEndAt == nil {
		t.Fatal("PlanEndAt should be set after re-plan finalises")
	}
	if !strings.Contains(got.Summary, "refreshed") {
		t.Fatalf("Summary = %q, want refreshed body", got.Summary)
	}
	if got.InvokedTool != "cursor" || got.InvokedModel != "sonnet-4" {
		t.Fatalf("tool/model = %q/%q, want refreshed values", got.InvokedTool, got.InvokedModel)
	}
}

// TestRun_FromTask_AgentError_LogsHelp drives the planErr branch in
// runReplanTask: agent failure must log the row as help and surface
// the error to the caller.
func TestRun_FromTask_AgentError_LogsHelp(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusPlanDone, "# req", nil)
	agent := newScriptedAgent()
	agent.planErr = errors.New("boom")
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusHelp {
		t.Fatalf("tasks = %+v, want one help-status row", tasks)
	}
}

// TestRun_FromTask_BackgroundSpawn_RecordsPID exercises the
// fire-and-forget headless spawn path through runReplanTask: a
// positive PID skips finishPlan and stamps the row's
// BackgroundPID.
func TestRun_FromTask_BackgroundSpawn_RecordsPID(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusPlanDone, "# req", nil)
	agent := newScriptedAgent()
	agent.planPID = 31415
	var stdout bytes.Buffer
	err := Run(context.Background(), Options{
		TaskID:      id,
		Interactive: false,
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout.String(), "running in background (PID=31415)") {
		t.Fatalf("stdout = %q, want banner with PID=31415", stdout.String())
	}
	if !strings.Contains(stdout.String(), "tail -f .j/tasks/") || !strings.Contains(stdout.String(), "/agent.log") {
		t.Fatalf("stdout = %q, want banner second row to invite `tail -f .j/tasks/<id>/agent.log`", stdout.String())
	}
	if !strings.Contains(stdout.String(), "┌") || !strings.Contains(stdout.String(), "└") {
		t.Fatalf("stdout = %q, want bordered box (┌ / └)", stdout.String())
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len = %d, want 1", len(tasks))
	}
	got := tasks[0]
	if got.Status != store.StatusPlanning {
		t.Fatalf("Status = %q, want planning (background row)", got.Status)
	}
	if got.BackgroundPID != 31415 {
		t.Fatalf("BackgroundPID = %d", got.BackgroundPID)
	}
}

// TestRun_FromTask_AgentSkipsRequirementsRead pins the warning
// branch when the agent claims success but does not produce
// requirements.md / plan.md. The row must still record plan-done
// (the lifecycle treats agent error as the only fatal signal).
func TestRun_FromTask_AgentSkipsRequirementsRead(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusPlanDone, "# req", nil)
	agent := newScriptedAgent()
	agent.skipWrite = true
	// Remove the seeded requirements.md so the post-run reads fail.
	reqPath := taskFilePath(t, id, store.RequirementsFileName)
	if err := os.Remove(reqPath); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "requirements.md") {
		t.Fatalf("stderr should warn about requirements.md: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "plan.md") {
		t.Fatalf("stderr should warn about plan.md: %q", stderr.String())
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusPlanDone {
		t.Fatalf("tasks = %+v, want one plan-done row", tasks)
	}
}

// TestRun_FromTask_NewResumeIDError warns and continues when the
// agent's resume-id minting fails on the re-plan path; the row
// still flips to plan-done because the resume cursor is best-effort.
func TestRun_FromTask_NewResumeIDError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusPlanDone, "# req", nil)
	agent := newScriptedAgent()
	agent.resumeErr = errors.New("create-chat down")
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "create-chat down") {
		t.Fatalf("stderr should warn about resume-id failure: %q", stderr.String())
	}
}

// TestRun_SourceTask_PicksAndReplans drives the full no-flag flow:
// SelectSource returns picker.SourceTask, the picker returns the only
// seeded id, runReplanTask completes successfully.
func TestRun_SourceTask_PicksAndReplans(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedReplanTask(t, store.StatusPlanDone, "# req\nbody", nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{source: picker.SourceTask, replanID: id}
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.replanCalls != 1 {
		t.Fatalf("PickReplanTask calls = %d, want 1", ui.replanCalls)
	}
	if agent.planned != 1 {
		t.Fatalf("agent.planned = %d, want 1", agent.planned)
	}
}

// TestRun_SourceTask_PickerError propagates the picker error and
// keeps the agent untouched.
func TestRun_SourceTask_PickerError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	_ = seedReplanTask(t, store.StatusPlanDone, "# req", nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{source: picker.SourceTask, replanErr: errors.New("pick boom")}
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "pick boom") {
		t.Fatalf("err = %v", err)
	}
	if agent.planned != 0 {
		t.Fatal("agent should not run when the picker errored")
	}
}

// TestBeginPlanTaskReuse_SeedsBeginIfMissing confirms the helper
// stamps a fresh PlanBeginAt when the existing row had none. This
// is the defensive branch behind the `if task.PlanBeginAt == nil`
// guard.
func TestBeginPlanTaskReuse_SeedsBeginIfMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	existing := store.Task{
		ID:           store.NewTaskID(),
		Status:       store.StatusPlanDone,
		InvokedTool:  "old",
		InvokedModel: "old",
		// PlanBeginAt intentionally nil.
	}
	if _, err := store.EnsureTaskDir(existing.ID); err != nil {
		t.Fatal(err)
	}
	lc := existing.BeginPlanReuse(io.Discard, "cursor", "sonnet-4", "resume-id")
	if lc == nil {
		t.Fatal("lifecycle = nil")
	}
	got := lc.Task()
	if got.PlanBeginAt == nil {
		t.Fatal("PlanBeginAt should be stamped when the row had none")
	}
	if got.InvokedTool != "cursor" || got.InvokedModel != "sonnet-4" {
		t.Fatalf("tool/model = %q/%q", got.InvokedTool, got.InvokedModel)
	}
	if got.PlanResumeCursor != "resume-id" {
		t.Fatalf("PlanResumeCursor = %q", got.PlanResumeCursor)
	}
	if got.Status != store.StatusPlanning {
		t.Fatalf("Status = %q, want planning", got.Status)
	}
}

// TestBeginPlanTaskReuse_PreservesExistingBegin pins the
// branch where the row already has a PlanBeginAt: the helper
// must keep it verbatim and clear PlanEndAt / DoneAt.
func TestBeginPlanTaskReuse_PreservesExistingBegin(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	original := time.Date(2023, 6, 7, 8, 9, 10, 0, time.UTC)
	existingEnd := original.Add(time.Minute)
	existing := store.Task{
		ID:           store.NewTaskID(),
		Status:       store.StatusPlanDone,
		InvokedTool:  "old",
		InvokedModel: "old",
		PlanBeginAt:  &original,
		PlanEndAt:    &existingEnd,
		DoneAt:       &existingEnd,
	}
	if _, err := store.EnsureTaskDir(existing.ID); err != nil {
		t.Fatal(err)
	}
	lc := existing.BeginPlanReuse(io.Discard, "cursor", "sonnet-4", "resume-id")
	if lc == nil {
		t.Fatal("lifecycle = nil")
	}
	got := lc.Task()
	if got.PlanBeginAt == nil || !got.PlanBeginAt.Equal(original) {
		t.Fatalf("PlanBeginAt = %v, want %v", got.PlanBeginAt, original)
	}
	if got.PlanEndAt != nil {
		t.Fatalf("PlanEndAt should be cleared, got %v", got.PlanEndAt)
	}
	if got.DoneAt != nil {
		t.Fatalf("DoneAt should be cleared, got %v", got.DoneAt)
	}
}
