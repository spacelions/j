package work

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

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
)

// seedResumableWork creates a task row plus the matching plan.md
// so RunResume's best-effort read does not warn. The default row
// is `work-done` with a non-empty WorkResumeCursor; tests override
// fields via mutate.
func seedResumableWork(t *testing.T, mutate func(*store.Task)) (string, *time.Time) {
	t.Helper()
	id := store.NewTaskID()
	if _, err := store.EnsureTaskDir(id); err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	taskDir := filepath.Join(tasksDir, id)
	if err := os.WriteFile(filepath.Join(taskDir, store.PlanFileName), []byte("1. step\n2. step\n"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	planBegin := time.Now().UTC().Add(-3 * time.Hour)
	planEnd := planBegin.Add(time.Hour)
	workBegin := planEnd.Add(time.Minute)
	workEnd := workBegin.Add(30 * time.Minute)
	task := store.Task{
		ID:               id,
		Status:           store.StatusWorkDone,
		InvokedTool:      "cursor",
		InvokedModel:     "sonnet-4",
		PlanResumeCursor: "plan-cursor",
		WorkResumeCursor: "work-cursor",
		Summary:          "seeded work",
		PlanBeginAt:      &planBegin,
		PlanEndAt:        &planEnd,
		WorkBeginAt:      &workBegin,
		WorkEndAt:        &workEnd,
	}
	if mutate != nil {
		mutate(&task)
	}
	dbPath, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	s := store.OpenTasks(dbPath)
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id, task.WorkBeginAt
}

func TestRunResume_Work_EmptySelector(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stdout bytes.Buffer

	err := RunResume(context.Background(), ResumeOptions{
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if got := strings.TrimRight(stdout.String(), "\n"); got != "J: there are no resumable sessions" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if agent.worked != 0 {
		t.Fatal("agent.Work should not be called when no sessions exist")
	}
	if ui.pickResumeCalls != 0 {
		t.Fatalf("PickWorkTask should not be called: pickResumeCalls=%d", ui.pickResumeCalls)
	}
}

// TestRunResume_Work_FromTaskHappyPath pins the --from-task flow:
// the agent receives Interactive=true and the recorded
// WorkResumeCursor + model, the row finishes as work-done, and the
// original WorkBeginAt is preserved. DoneAt stays nil.
func TestRunResume_Work_FromTaskHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, originalBegin := seedResumableWork(t, nil)
	agent := newScriptedAgent()
	var stdout bytes.Buffer

	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if !strings.Contains(stdout.String(), "work resume on task "+id) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if agent.worked != 1 {
		t.Fatalf("agent.Work calls = %d, want 1", agent.worked)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("Interactive should be true: %+v", agent.lastReq)
	}
	if agent.lastReq.ResumeChatID != "work-cursor" {
		t.Fatalf("ResumeChatID = %q, want work-cursor", agent.lastReq.ResumeChatID)
	}
	if !agent.lastReq.Resume {
		t.Fatalf("Resume should be true on resume: %+v", agent.lastReq)
	}
	if agent.lastReq.Model != "sonnet-4" {
		t.Fatalf("Model = %q, want sonnet-4", agent.lastReq.Model)
	}
	if agent.resumeIDed != 0 {
		t.Fatalf("NewResumeID should not be invoked on resume; calls=%d", agent.resumeIDed)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].ID != id {
		t.Fatalf("tasks = %+v", tasks)
	}
	got := tasks[0]
	if got.Status != store.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
	if got.WorkBeginAt == nil || !got.WorkBeginAt.Equal(*originalBegin) {
		t.Fatalf("WorkBeginAt = %v, want preserved %v", got.WorkBeginAt, originalBegin)
	}
	if got.WorkEndAt == nil {
		t.Fatalf("WorkEndAt should be bumped: %+v", got)
	}
	if got.DoneAt != nil {
		t.Fatalf("DoneAt should remain nil: %v", got.DoneAt)
	}
}

func TestRunResume_Work_FromTaskMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	agent := newScriptedAgent()
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: "missing",
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), `task "missing" not found`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunResume_Work_FromTaskNoSession(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableWork(t, func(task *store.Task) { task.WorkResumeCursor = "" })
	agent := newScriptedAgent()
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "has no work session") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunResume_Work_SelectorPicksSecond pins the multi-task path:
// scripted UI returns the second eligible task and the agent
// receives that task's WorkResumeCursor.
func TestRunResume_Work_SelectorPicksSecond(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id1, _ := seedResumableWork(t, func(task *store.Task) { task.WorkResumeCursor = "first-cursor" })
	id2, _ := seedResumableWork(t, func(task *store.Task) { task.WorkResumeCursor = "second-cursor" })
	agent := newScriptedAgent()
	ui := &scriptedUI{resumePicked: id2}

	err := RunResume(context.Background(), ResumeOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if ui.pickResumeCalls != 1 {
		t.Fatalf("PickWorkTask calls = %d, want 1", ui.pickResumeCalls)
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickPlanTask should not be called on resume; calls=%d", ui.pickCalls)
	}
	if agent.lastReq.ResumeChatID != "second-cursor" {
		t.Fatalf("ResumeChatID = %q, want second-cursor (id1=%s)", agent.lastReq.ResumeChatID, id1)
	}
}

// TestRunResume_Work_PickerReturnsUnknownID covers the post-loop
// branch where the picker returns an off-list id (impossible with
// the real huh widget but reachable via a scripted fake).
func TestRunResume_Work_PickerReturnsUnknownID(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedResumableWork(t, nil)
	seedResumableWork(t, nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{resumePicked: "ghost-id"}
	err := RunResume(context.Background(), ResumeOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), `task "ghost-id" not found`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunResume_Work_UnknownTool(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableWork(t, func(task *store.Task) { task.InvokedTool = "ghost" })
	agent := newScriptedAgent()
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown tool "ghost"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunResume_Work_AgentError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableWork(t, nil)
	agent := newScriptedAgent()
	agent.workErr = errors.New("work boom")

	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "work boom") {
		t.Fatalf("err = %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusHelp {
		t.Fatalf("tasks = %+v, want one help-status row", tasks)
	}
	if tasks[0].WorkEndAt == nil {
		t.Fatalf("WorkEndAt should be bumped on failure: %+v", tasks[0])
	}
	if tasks[0].DoneAt != nil {
		t.Fatalf("DoneAt should remain nil: %v", tasks[0].DoneAt)
	}
}

// TestRunResume_Work_StatusWorkingIsResumable pins the permissive
// eligibility filter: a task whose status is `working` (which the
// non-resume `j work --from-task` path rejects via validateForWork)
// is still resumable as long as its WorkResumeCursor is non-empty.
func TestRunResume_Work_StatusWorkingIsResumable(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableWork(t, func(task *store.Task) { task.Status = store.StatusWorking })
	agent := newScriptedAgent()
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if agent.worked != 1 {
		t.Fatalf("agent.Work calls = %d, want 1 (working should be resumable)", agent.worked)
	}
}

// TestRunResume_Work_AutoPicksSingle exercises the case-1 branch
// in resolveResumeTask: a single eligible task is picked
// automatically without consulting the UI. It also deletes
// plan.md before invoking RunResume to confirm the orchestrator
// does not stat the plan body itself (the agent reads it from
// disk via the cited PlanPath).
func TestRunResume_Work_AutoPicksSingle(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableWork(t, nil)
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	if err := os.Remove(filepath.Join(tasksDir, id, store.PlanFileName)); err != nil {
		t.Fatalf("Remove plan.md: %v", err)
	}
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	err = RunResume(context.Background(), ResumeOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if ui.pickResumeCalls != 0 {
		t.Fatalf("picker should not be called for a single task, calls = %d", ui.pickResumeCalls)
	}
	if agent.worked != 1 {
		t.Fatalf("agent.Work calls = %d, want 1", agent.worked)
	}
	// The resume flow now tells the agent to read plan.md from
	// disk; the orchestrator does not pre-read the body, so a
	// missing plan.md no longer surfaces here. PlanPath must
	// still be set so the agent knows which file to read.
	if agent.lastReq.PlanPath == "" {
		t.Fatalf("PlanPath should be populated even when plan.md is missing, got %q", agent.lastReq.PlanPath)
	}
}

func TestRunResume_Work_NoAgents(t *testing.T) {
	err := RunResume(context.Background(), ResumeOptions{Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunResume_Work_PickerError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedResumableWork(t, nil)
	seedResumableWork(t, nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{resumeErr: errors.New("picker boom")}
	err := RunResume(context.Background(), ResumeOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "picker boom") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunResume_Work_PickerCancelled covers the cancel signal
// from the unified picker contract: a user-abort (or empty
// resumePicked) surfaced from PickWorkTask returns ok=false and
// RunResume must exit cleanly with nil. The agent must never be
// invoked.
func TestRunResume_Work_PickerCancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedResumableWork(t, nil)
	seedResumableWork(t, nil)
	agent := newScriptedAgent()
	err := RunResume(context.Background(), ResumeOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil (abort exits cleanly)", err)
	}
	if agent.worked != 0 {
		t.Fatal("agent should not be touched after cancel")
	}
}

// TestRunResume_Work_AppliesDefaults exercises ResumeOptions.withDefaults
// (the nil-stdin / nil-stdout / nil-stderr / nil-UI branches).
func TestRunResume_Work_AppliesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if err := RunResume(context.Background(), ResumeOptions{
		Agents: []codingagents.Agent{newScriptedAgent()},
	}); err != nil {
		t.Fatalf("RunResume: %v", err)
	}
}

// TestRunResume_Work_ListDecodeError plants a malformed task.toml
// so listResumableTasks returns a decode error.
func TestRunResume_Work_ListDecodeError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	dbPath, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatal(err)
	}
	badDir := filepath.Join(dbPath, "bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "task.toml"), []byte("not = valid = toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = RunResume(context.Background(), ResumeOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "decode task") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunResume_Work_FromTaskDecodeError plants a bad JSON
// payload under a known task id and exercises the
// resolveResumeByID branch that returns a non-fs.ErrNotExist
// error from GetTask.
func TestRunResume_Work_FromTaskDecodeError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	testutil.SeedRawTaskFile(t, "broken", []byte("not = valid = toml"))
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: "broken",
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "decode task") {
		t.Fatalf("err = %v, want decode-task error from GetTask", err)
	}
}

// TestNewResumeCmd_FromTaskFlowsToViper covers the cobra wiring.
func TestNewResumeCmd_FromTaskFlowsToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := newResumeCmd()
	if err := cmd.Flags().Set("from-task", "abc"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("work.resume.from_task"); got != "abc" {
		t.Errorf("work.resume.from_task = %q, want %q", got, "abc")
	}
}

func TestNewResumeCmd_FromTaskEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("WORK_RESUME_FROM_TASK", "env-task")

	_ = newResumeCmd()
	if got := viper.GetString("work.resume.from_task"); got != "env-task" {
		t.Errorf("work.resume.from_task = %q, want %q", got, "env-task")
	}
}

func TestNewResumeCmd_RegistersOnlyFromTask(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := newResumeCmd()
	if cmd.Use != "resume" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		names = append(names, f.Name)
	})
	if len(names) != 1 || names[0] != "from-task" {
		t.Fatalf("flags = %v, want only [from-task]", names)
	}
	for _, banned := range []string{"interactive", "from-settings", "from-file"} {
		if cmd.Flags().Lookup(banned) != nil {
			t.Fatalf("--%s should not be registered on `j work resume`", banned)
		}
	}
}

// TestNewResumeCmd_RunEPropagates exercises the RunE closure by
// pointing --from-task at a missing id; the closure must build an
// Options struct from viper and call RunResume.
func TestNewResumeCmd_RunEPropagates(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	mustInit(t)

	cmd := newResumeCmd()
	if err := cmd.Flags().Set("from-task", "missing-id"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), `task "missing-id" not found`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunResume_Work_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	var found bool
	for _, sub := range parent.Commands() {
		if sub.Name() == "resume" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("`j work resume` should be registered as a child of `j work`")
	}
}

// TestBeginWorkTaskResume_PreservesCursorAndBegin pins the lifecycle
// helper directly: WorkResumeCursor is preserved verbatim and the
// existing WorkBeginAt is not overwritten.
func TestBeginWorkTaskResume_PreservesCursorAndBegin(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableWork(t, nil)
	dbPath, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatal(err)
	}
	s := store.OpenTasks(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	preBegin := existing.WorkBeginAt
	preCursor := existing.WorkResumeCursor

	lc := existing.BeginWorkResume(io.Discard)
	lc.Finish(nil)

	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].ID != id {
		t.Fatalf("tasks = %+v", tasks)
	}
	got := tasks[0]
	if got.WorkResumeCursor != preCursor {
		t.Fatalf("WorkResumeCursor changed: got %q, want %q", got.WorkResumeCursor, preCursor)
	}
	if got.WorkBeginAt == nil || !got.WorkBeginAt.Equal(*preBegin) {
		t.Fatalf("WorkBeginAt = %v, want preserved %v", got.WorkBeginAt, preBegin)
	}
}

// TestRunResume_Work_AlwaysInteractive pins the always-interactive
// contract for `j work resume`: regardless of the worker bucket's
// stored `interactive` value (or absence thereof), resume forces
// Interactive=true so the user can iterate via the TUI. Headless
// resume has no stdin path back to the human, so respecting a
// stored `interactive=false` would dead-end any clarification turn.
func TestRunResume_Work_AlwaysInteractive(t *testing.T) {
	cases := []struct {
		name        string
		seedBucket  bool
		bucketValue string
	}{
		{name: "stored-true", seedBucket: true, bucketValue: "true"},
		{name: "stored-false", seedBucket: true, bucketValue: "false"},
		{name: "bucket-empty", seedBucket: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			mustInit(t)
			if tc.seedBucket {
				seedWorkerInteractive(t, tc.bucketValue)
			}
			id, _ := seedResumableWork(t, nil)
			agent := newScriptedAgent()
			if err := RunResume(context.Background(), ResumeOptions{
				TaskID: id,
				Stdout: io.Discard,
				Stderr: io.Discard,
				Agents: []codingagents.Agent{agent},
				UI:     &scriptedUI{},
			}); err != nil {
				t.Fatalf("RunResume: %v", err)
			}
			if !agent.lastReq.Interactive {
				t.Fatalf("Interactive = false, want true (resume always forces interactive, bucket=%q)", tc.bucketValue)
			}
		})
	}
}

// TestRunResume_Work_ForwardsMustRead pins AC: `j work resume`
// loads the project's mustRead setting and threads it into
// WorkRequest.MustRead so the resume turn inherits the same
// project-wide context the first run had. Without this,
// BuildWorkerResume would silently render a must-read-less prompt.
func TestRunResume_Work_ForwardsMustRead(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedProjectMustRead(t, "AGENTS.md;CLAUDE.md")
	id, _ := seedResumableWork(t, nil)
	agent := newScriptedAgent()
	if err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	want := []string{"AGENTS.md", "CLAUDE.md"}
	if got := agent.lastReq.MustRead; len(got) != len(want) {
		t.Fatalf("MustRead = %v, want %v", got, want)
	} else {
		for i, v := range want {
			if got[i] != v {
				t.Fatalf("MustRead[%d] = %q, want %q (case must be preserved)", i, got[i], v)
			}
		}
	}
}

// TestRunResume_Work_MustReadUnsetYieldsNil covers the
// no-bucket-entry branch of resolver.MustRead: when the project has
// no mustRead setting, the resume call must still proceed and pass a
// nil/empty slice (mirroring what the first-run work flow does).
func TestRunResume_Work_MustReadUnsetYieldsNil(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableWork(t, nil)
	agent := newScriptedAgent()
	if err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if len(agent.lastReq.MustRead) != 0 {
		t.Fatalf("MustRead = %v, want empty when bucket has no entry", agent.lastReq.MustRead)
	}
}

// seedProjectMustRead writes a `;`-separated must-read list under the
// project bucket so resume's resolver.MustRead returns the parsed
// slice. Mirrors the helper in internal/cli/plan/resume_test.go.
func seedProjectMustRead(t *testing.T, value string) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.EnsureBucket(store.BucketProject); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	if err := s.Put(store.BucketProject, resolver.KeyMustRead, value); err != nil {
		t.Fatalf("Put mustRead: %v", err)
	}
}

// seedWorkerInteractive writes a literal `interactive` value into the
// worker bucket. Reused by TestRunResume_Work_AlwaysInteractive to
// prove the stored value is intentionally ignored on resume.
func seedWorkerInteractive(t *testing.T, value string) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.EnsureBucket(store.BucketWorker); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	if err := s.Put(store.BucketWorker, "interactive", value); err != nil {
		t.Fatalf("Put interactive: %v", err)
	}
}

// TestBeginWorkTaskResume_NilWorkBeginAtStampsFreshOne covers the
// fallback path where an existing task somehow has a nil
// WorkBeginAt; the helper must mint a fresh timestamp.
func TestBeginWorkTaskResume_NilWorkBeginAtStampsFreshOne(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableWork(t, func(task *store.Task) { task.WorkBeginAt = nil })
	dbPath, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatal(err)
	}
	s := store.OpenTasks(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	lc := existing.BeginWorkResume(io.Discard)
	lc.Finish(nil)

	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].WorkBeginAt == nil {
		t.Fatalf("WorkBeginAt should be stamped: %+v", tasks)
	}
}
