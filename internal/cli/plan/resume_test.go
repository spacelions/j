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

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// seedResumableTask creates a task row plus the matching
// .j/tasks/<id>/{requirements.md,plan.md} files. Returns the id and
// the original PlanBeginAt so callers can assert it survived the
// resume lifecycle. The status / cursor / tool / model defaults match
// a typical post-`j plan` row; tests override fields via mutate.
func seedResumableTask(t *testing.T, mutate func(*tasks.Task)) (string, *time.Time) {
	t.Helper()
	id := tasks.NewTaskID()
	if _, err := tasks.EnsureDir(id); err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	taskDir := filepath.Join(tasksDir, id)
	if err := os.WriteFile(filepath.Join(taskDir, tasks.RequirementsFileName), []byte("# req\nbody"), 0o644); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, tasks.PlanFileName), []byte("1. step\n"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	begin := time.Now().UTC().Add(-2 * time.Hour)
	end := begin.Add(time.Hour)
	task := tasks.Task{
		ID:               id,
		Status:           tasks.StatusPlanDone,
		InvokedTool:      "cursor",
		InvokedModel:     "sonnet-4",
		PlanResumeCursor: "abc",
		Summary:          "seeded summary",
		PlanBeginAt:      &begin,
		PlanEndAt:        &end,
	}
	if mutate != nil {
		mutate(&task)
	}
	dbPath, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	s := tasks.Open(dbPath)
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id, t.PlanBeginAt
}

// TestRunResume_EmptySelector pins AC#A2: an initialised project
// with no resumable tasks prints exactly the no-sessions line and
// returns nil.
func TestRunResume_EmptySelector(t *testing.T) {
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
	if agent.planned != 0 {
		t.Fatal("agent.Plan should not be called when no sessions exist")
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickPlanTask should not be called: pickCalls=%d", ui.pickCalls)
	}
}

// TestRunResume_FromTaskHappyPath pins the --from-task flow: the
// task row is loaded, the agent receives Interactive=true and the
// recorded cursor + model, and the row finishes as plan-done with
// the original PlanBeginAt preserved.
func TestRunResume_FromTaskHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, originalBegin := seedResumableTask(t, nil)
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
	if !strings.Contains(stdout.String(), "plan resume on task "+id) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if agent.planned != 1 {
		t.Fatalf("agent.Plan calls = %d, want 1", agent.planned)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("Interactive should be true: %+v", agent.lastReq)
	}
	if agent.lastReq.ResumeChatID != "abc" {
		t.Fatalf("ResumeChatID = %q, want abc", agent.lastReq.ResumeChatID)
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
	if got.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.PlanBeginAt == nil || !got.PlanBeginAt.Equal(*originalBegin) {
		t.Fatalf("PlanBeginAt = %v, want preserved %v", got.PlanBeginAt, originalBegin)
	}
	if got.PlanEndAt == nil {
		t.Fatalf("PlanEndAt should be bumped on success: %+v", got)
	}
}

// TestRunResume_FromTaskMissing pins the not-found error when the
// id supplied via --from-task does not exist in bbolt.
func TestRunResume_FromTaskMissing(t *testing.T) {
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
	if agent.planned != 0 {
		t.Fatal("agent.Plan should not be called when task is missing")
	}
}

// TestRunResume_FromTaskNoSession pins the empty-cursor error: a
// task that exists but has no PlanResumeCursor cannot be resumed.
func TestRunResume_FromTaskNoSession(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableTask(t, func(task *tasks.Task) { t.PlanResumeCursor = "" })
	agent := newScriptedAgent()
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "has no plan session") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunResume_SelectorPicksSecond pins the multi-task path: two
// eligible tasks, scripted UI picks the second, and the agent
// receives that task's cursor.
func TestRunResume_SelectorPicksSecond(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id1, _ := seedResumableTask(t, func(task *tasks.Task) { t.PlanResumeCursor = "first-cursor" })
	id2, _ := seedResumableTask(t, func(task *tasks.Task) { t.PlanResumeCursor = "second-cursor" })
	agent := newScriptedAgent()
	ui := &scriptedUI{pickedID: id2}

	err := RunResume(context.Background(), ResumeOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickPlanTask calls = %d, want 1", ui.pickCalls)
	}
	if agent.lastReq.ResumeChatID != "second-cursor" {
		t.Fatalf("ResumeChatID = %q, want second-cursor", agent.lastReq.ResumeChatID)
	}
	tasks := readTasks(t)
	for _, t := range tasks {
		if t.ID == id2 && t.Status != tasks.StatusPlanDone {
			t.Fatalf("picked task should be plan-done: %+v", task)
		}
		if t.ID == id1 && t.Status != tasks.StatusPlanDone {
			t.Fatalf("non-picked task should remain plan-done: %+v", task)
		}
	}
}

// TestRunResume_PickerReturnsUnknownID pins the post-loop branch
// where the UI returns a label that does not match any known task
// id (impossible with the real huh widget but reachable via a
// scripted fake; covering the branch closes the safety net).
func TestRunResume_PickerReturnsUnknownID(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedResumableTask(t, nil)
	seedResumableTask(t, nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{pickedID: "ghost-id"}
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

// TestRunResume_UnknownTool pins the lookup-by-tool error when the
// recorded InvokedTool does not match any wired-in agent.
func TestRunResume_UnknownTool(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableTask(t, func(task *tasks.Task) { t.InvokedTool = "ghost" })
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
	if agent.planned != 0 {
		t.Fatal("agent.Plan should not be called when tool is unknown")
	}
}

// TestRunResume_AgentError stamps `help` on the task row and
// returns the agent error verbatim.
func TestRunResume_AgentError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableTask(t, nil)
	agent := newScriptedAgent()
	agent.planErr = errors.New("plan boom")

	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "plan boom") {
		t.Fatalf("err = %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != tasks.StatusHelp {
		t.Fatalf("tasks = %+v, want one help-status row", tasks)
	}
	if tasks[0].PlanEndAt == nil {
		t.Fatalf("PlanEndAt should be bumped on failure too: %+v", tasks[0])
	}
}

// TestRunResume_NoAgents pins the no-agents-configured branch.
func TestRunResume_NoAgents(t *testing.T) {
	err := RunResume(context.Background(), ResumeOptions{Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunResume_PickerError surfaces the UI picker error verbatim.
func TestRunResume_PickerError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedResumableTask(t, nil)
	seedResumableTask(t, nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{pickErr: errors.New("picker boom")}
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

// TestRunResume_PickerCancelled covers the cancel signal from
// the unified picker contract: a user-abort (or empty
// pickedID) surfaced from PickPlanTask returns ok=false and
// RunResume must exit cleanly with nil. The agent must never be
// invoked.
func TestRunResume_PickerCancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedResumableTask(t, nil)
	seedResumableTask(t, nil)
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
	if agent.planned != 0 {
		t.Fatal("agent should not be touched after cancel")
	}
}

// TestRunResume_SelectorAutoPicksSingle pins the one-task short-
// circuit: when exactly one resumable task exists, RunResume skips
// the picker and resumes it directly.
func TestRunResume_SelectorAutoPicksSingle(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableTask(t, nil)
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	err := RunResume(context.Background(), ResumeOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if ui.pickCalls != 0 {
		t.Fatalf("picker should be skipped for single task, calls=%d", ui.pickCalls)
	}
	if agent.lastReq.ResumeChatID != "abc" {
		t.Fatalf("ResumeChatID = %q, want abc (auto-picked single task %s)", agent.lastReq.ResumeChatID, id)
	}
}

// TestRunResume_AgentMissingFilesWarn pins the post-run warning
// path: the agent claimed success but neither requirements.md nor
// plan.md exists, so two warnings surface on stderr and the task
// still ends up plan-done.
func TestRunResume_AgentMissingFilesWarn(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableTask(t, nil)
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(tasksDir, id)
	if err := os.Remove(filepath.Join(taskDir, tasks.RequirementsFileName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(taskDir, tasks.PlanFileName)); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	agent.skipWrite = true
	var stderr bytes.Buffer

	err = RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if !strings.Contains(stderr.String(), "requirements.md") || !strings.Contains(stderr.String(), "plan.md") {
		t.Fatalf("stderr should warn about both files: %q", stderr.String())
	}
}

// TestRunResume_AppliesDefaults exercises ResumeOptions.withDefaults
// (the nil-stdin / nil-stdout / nil-stderr / nil-UI branches) by
// passing an Options with only Agents set and the no-resumable-
// sessions path so we never hit stdin.
func TestRunResume_AppliesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if err := RunResume(context.Background(), ResumeOptions{
		Agents: []codingagents.Agent{newScriptedAgent()},
	}); err != nil {
		t.Fatalf("RunResume: %v", err)
	}
}

// TestRunResume_ListDecodeError plants a bad JSON payload in the
// tasks bucket so listResumableTasks returns a decode error.
func TestRunResume_ListDecodeError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	dbPath, err := tasks.DefaultDir()
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

// TestRunResume_FromTaskDecodeError plants a bad JSON payload
// under a known id so resolveResumeByID's GetTask returns a
// non-fs.ErrNotExist error path.
func TestRunResume_FromTaskDecodeError(t *testing.T) {
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

// TestPlanResumeBegin_NilBeginStampsFresh covers the fallback
// branch where an existing task has a nil PlanBeginAt: the helper
// must mint a fresh timestamp so the row is not left in a
// half-stamped state.
func TestPlanResumeBegin_NilBeginStampsFresh(t *testing.T) {
	got := planResumeBegin(tasks.Task{ID: "x", Status: tasks.StatusPlanDone})
	if got.PlanBeginAt == nil {
		t.Fatal("PlanBeginAt should be stamped when nil")
	}
	if got.Status != tasks.StatusPlanning {
		t.Fatalf("Status = %q, want planning", got.Status)
	}
	if got.PlanEndAt != nil {
		t.Fatalf("PlanEndAt should be cleared: %v", got.PlanEndAt)
	}
}

// TestPlanResumeFinish_StatusBranches pins both terminal statuses
// in isolation.
func TestPlanResumeFinish_StatusBranches(t *testing.T) {
	base := tasks.Task{ID: "x", Status: tasks.StatusPlanning}
	if got := planResumeFinish(base, nil, "# ok", "plan", "/tmp/x.md"); got.Status != tasks.StatusPlanDone {
		t.Fatalf("success Status = %q", got.Status)
	}
	if got := planResumeFinish(base, errors.New("boom"), "", "", ""); got.Status != tasks.StatusHelp {
		t.Fatalf("error Status = %q", got.Status)
	}
}

// TestNewResumeCmd_FromTaskFlowsToViper mirrors
// TestNew_FromTaskFlowsToViper for the resume cobra child: setting
// the flag must populate the new viper key.
func TestNewResumeCmd_FromTaskFlowsToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := newResumeCmd()
	if err := cmd.Flags().Set("from-task", "abc"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("plan.resume.from_task"); got != "abc" {
		t.Errorf("plan.resume.from_task = %q, want %q", got, "abc")
	}
}

// TestNewResumeCmd_FromTaskEnv covers the env-var binding so
// PLAN_RESUME_FROM_TASK works without a flag.
func TestNewResumeCmd_FromTaskEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("PLAN_RESUME_FROM_TASK", "env-task")

	_ = newResumeCmd()
	if got := viper.GetString("plan.resume.from_task"); got != "env-task" {
		t.Errorf("plan.resume.from_task = %q, want %q", got, "env-task")
	}
}

// TestNewResumeCmd_RegistersOnlyFromTask asserts the resume
// subcommand carries exactly one flag (--from-task), matching the
// cobra surface mandated by the plan.
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
			t.Fatalf("--%s should not be registered on `j plan resume`", banned)
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

// TestRunResume_AlwaysInteractive pins AC#A1: resume forces
// Interactive=true on every run regardless of the planner bucket's
// stored value (or absence thereof). The previous bucket-driven
// behaviour was the root cause of the help-status recovery bug —
// a help row whose first run went headless would otherwise re-spawn
// headless and the user could never answer the clarification turn.
//
// Sub-cases cover the three observable bucket states (`true`,
// `false`, unset); the "stored=false" case is the primary
// regression guard. A side assertion checks resume is read-only:
// it must not populate the planner bucket's tool/model entries.
func TestRunResume_AlwaysInteractive(t *testing.T) {
	cases := []struct {
		name           string
		seedBucket     bool
		bucketValue    string
		assertReadOnly bool
	}{
		{name: "stored-true", seedBucket: true, bucketValue: "true", assertReadOnly: true},
		{name: "stored-false", seedBucket: true, bucketValue: "false"},
		{name: "bucket-empty", seedBucket: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			mustInit(t)
			if tc.seedBucket {
				seedPlannerInteractive(t, tc.bucketValue)
			}
			id, _ := seedResumableTask(t, nil)
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
			if tc.assertReadOnly {
				tool, model := readPlannerToolModel(t)
				if tool != "" || model != "" {
					t.Fatalf("planner bucket gained tool/model from resume: tool=%q model=%q", tool, model)
				}
			}
		})
	}
}

// TestRunResume_ForwardsMustRead pins AC: resume loads the project's
// mustRead setting and threads it into PlanRequest.MustRead so the
// resume turn inherits the same project-wide context the first run
// had. Without this, BuildPlannerResume would silently render a
// must-read-less prompt on resume.
func TestRunResume_ForwardsMustRead(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedProjectMustRead(t, "AGENTS.md;CLAUDE.md")
	id, _ := seedResumableTask(t, nil)
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

// TestRunResume_MustReadUnsetYieldsNil covers the no-bucket-entry
// branch of resolver.MustRead: when the project has no
// mustRead setting, the resume call must still proceed and pass a
// nil/empty slice (mirroring what the first-run plan flow does).
func TestRunResume_MustReadUnsetYieldsNil(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableTask(t, nil)
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

// seedPlannerInteractive writes a literal `interactive` value into the
// planner bucket. Reused by TestRunResume_AlwaysInteractive to prove
// the stored value is intentionally ignored on resume.
func seedPlannerInteractive(t *testing.T, value string) {
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
	if err := s.EnsureBucket(store.BucketPlanner); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	if err := s.Put(store.BucketPlanner, "interactive", value); err != nil {
		t.Fatalf("Put interactive: %v", err)
	}
}

// seedProjectMustRead writes a `;`-separated must-read list under the
// project bucket so resume's resolver.MustRead returns the
// parsed slice. Mirrors preflight's putMustRead helper without
// depending on it across packages.
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
		t.Fatalf("Put must_read: %v", err)
	}
}

// readPlannerToolModel returns the tool/model entries currently stored
// in the planner bucket. Used to assert resume does not overwrite the
// bucket.
func readPlannerToolModel(t *testing.T) (string, string) {
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
	tool, _, _ := s.Get(store.BucketPlanner, "tool")
	model, _, _ := s.Get(store.BucketPlanner, "model")
	return tool, model
}

// TestRunResume_RegisteredAsChild verifies `j plan resume` exists
// as a cobra child of `j plan`, satisfying AC#A1.
func TestRunResume_RegisteredAsChild(t *testing.T) {
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
		t.Fatal("`j plan resume` should be registered as a child of `j plan`")
	}
}
