package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
)

// seedResumableVerify creates a task row plus the matching
// requirements / plan / verifier_findings markdown files. The
// default row is `verify-done` with a non-empty
// VerifyResumeCursor; tests override fields via mutate.
func seedResumableVerify(t *testing.T, mutate func(*store.Task)) (string, *time.Time) {
	t.Helper()
	id := store.NewTaskID()
	if _, err := store.EnsureTaskDir(id); err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	taskDir := filepath.Join(mustTasksDir(t), id)
	if err := os.WriteFile(filepath.Join(taskDir, store.PlanFileName), []byte("1. step\n"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, store.RequirementsFileName), []byte("# req\nbody"), 0o644); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, store.VerifierFindingsFileName), []byte("- prior\nVERDICT: FAIL\n"), 0o644); err != nil {
		t.Fatalf("write findings: %v", err)
	}
	planBegin := time.Now().UTC().Add(-3 * time.Hour)
	planEnd := planBegin.Add(time.Hour)
	workBegin := planEnd.Add(time.Minute)
	workEnd := workBegin.Add(30 * time.Minute)
	verifyBegin := workEnd.Add(time.Minute)
	verifyEnd := verifyBegin.Add(time.Minute)
	task := store.Task{
		ID:                 id,
		Status:             store.StatusVerifyDone,
		InvokedTool:        "cursor",
		InvokedModel:       "sonnet-4",
		PlanResumeCursor:   "plan-cursor",
		WorkResumeCursor:   "work-cursor",
		VerifyResumeCursor: "verify-cursor",
		Summary:            "seeded verify",
		PlanBeginAt:        &planBegin,
		PlanEndAt:          &planEnd,
		WorkBeginAt:        &workBegin,
		WorkEndAt:          &workEnd,
		VerifyBeginAt:      &verifyBegin,
		VerifyEndAt:        &verifyEnd,
	}
	if mutate != nil {
		mutate(&task)
	}
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id, task.VerifyBeginAt
}

func mustTasksDir(t *testing.T) string {
	t.Helper()
	d, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	return d
}

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
	if got := strings.TrimRight(stdout.String(), "\n"); got != "J: there are no resumable verify sessions" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if len(agent.verifiedReqs) != 0 {
		t.Fatal("agent.Verify should not be called when no sessions exist")
	}
}

// TestRunResume_FromTaskHappyPath pins the --from-task flow: the
// agent receives Interactive=true, the recorded
// VerifyResumeCursor + model, and (because the verifier writes a
// PASS verdict by default) the row finishes as completed.
func TestRunResume_FromTaskHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, originalBegin := seedResumableVerify(t, nil)
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
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
	if !strings.Contains(stdout.String(), "verify resume on task "+id) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if len(agent.verifiedReqs) != 1 {
		t.Fatalf("verify calls = %d, want 1", len(agent.verifiedReqs))
	}
	req := agent.verifiedReqs[0]
	if !req.Interactive {
		t.Fatalf("Interactive should be true: %+v", req)
	}
	if req.ResumeChatID != "verify-cursor" {
		t.Fatalf("ResumeChatID = %q", req.ResumeChatID)
	}
	if !req.Resume {
		t.Fatalf("Resume should be true: %+v", req)
	}
	if req.Model != "sonnet-4" {
		t.Fatalf("Model = %q", req.Model)
	}
	if req.RequirementsPath == "" || req.PlanPath == "" || req.VerifierFindingsOutputPath == "" {
		t.Fatalf("paths should be populated so the agent can read them from disk: %+v", req)
	}
	if agent.resumeIDed != 0 {
		t.Fatalf("NewResumeID should not be invoked on resume; calls=%d", agent.resumeIDed)
	}
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusCompleted {
		t.Fatalf("Status = %q, want completed (PASS verdict)", tasks[0].Status)
	}
	if tasks[0].VerifyBeginAt == nil || !tasks[0].VerifyBeginAt.Equal(*originalBegin) {
		t.Fatalf("VerifyBeginAt should be preserved: %v vs %v", tasks[0].VerifyBeginAt, originalBegin)
	}
}

// TestRunResume_FromTaskMissing surfaces the not-found error.
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
}

// TestRunResume_FromTaskNoSession surfaces the empty-cursor error.
func TestRunResume_FromTaskNoSession(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, func(task *store.Task) { task.VerifyResumeCursor = "" })
	agent := newScriptedAgent()
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "has no verify session") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunResume_SelectorPicksSecond multi-task path.
func TestRunResume_SelectorPicksSecond(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id1, _ := seedResumableVerify(t, func(task *store.Task) { task.VerifyResumeCursor = "first-cursor" })
	id2, _ := seedResumableVerify(t, func(task *store.Task) { task.VerifyResumeCursor = "second-cursor" })
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
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
		t.Fatalf("PickVerifyTask calls = %d, want 1", ui.pickResumeCalls)
	}
	if agent.verifiedReqs[0].ResumeChatID != "second-cursor" {
		t.Fatalf("ResumeChatID = %q, want second-cursor (id1=%s)", agent.verifiedReqs[0].ResumeChatID, id1)
	}
}

// TestRunResume_PickerReturnsUnknownID covers the post-loop branch.
func TestRunResume_PickerReturnsUnknownID(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedResumableVerify(t, nil)
	seedResumableVerify(t, nil)
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

func TestRunResume_UnknownTool(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, func(task *store.Task) { task.InvokedTool = "ghost" })
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

// TestRunResume_AgentError pins the verify-error branch on resume.
func TestRunResume_AgentError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, nil)
	agent := newScriptedAgent()
	agent.verifyErr = errors.New("verify boom")

	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "verify boom") {
		t.Fatalf("err = %v", err)
	}
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusHelp {
		t.Fatalf("Status = %q, want help", tasks[0].Status)
	}
}

// TestRunResume_StatusCompletedIsResumable pins the permissive
// eligibility filter.
func TestRunResume_StatusCompletedIsResumable(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, func(task *store.Task) { task.Status = store.StatusCompleted })
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
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
	if len(agent.verifiedReqs) != 1 {
		t.Fatalf("verify calls = %d, want 1 (completed should be resumable)", len(agent.verifiedReqs))
	}
}

// TestRunResume_AutoPicksSingle exercises the case-1 branch.
// Deletes findings.md before invoking RunResume to confirm the
// orchestrator no longer pre-reads the findings body — the agent
// now reads verifier_findings.md from disk via the cited path.
func TestRunResume_AutoPicksSingle(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, nil)
	if err := os.Remove(filepath.Join(mustTasksDir(t), id, store.VerifierFindingsFileName)); err != nil {
		t.Fatalf("remove findings: %v", err)
	}
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
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
	if ui.pickResumeCalls != 0 {
		t.Fatalf("picker should not be called for a single task, calls = %d", ui.pickResumeCalls)
	}
	if len(agent.verifiedReqs) != 1 {
		t.Fatalf("verify calls = %d, want 1", len(agent.verifiedReqs))
	}
	if agent.verifiedReqs[0].VerifierFindingsOutputPath == "" {
		t.Fatalf("VerifierFindingsOutputPath should be populated even when findings.md is missing, got %q",
			agent.verifiedReqs[0].VerifierFindingsOutputPath)
	}
}

func TestRunResume_NoAgents(t *testing.T) {
	err := RunResume(context.Background(), ResumeOptions{Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunResume_PickerError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedResumableVerify(t, nil)
	seedResumableVerify(t, nil)
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

// TestRunResume_PickerCancelled covers the cancel signal from
// the unified picker contract: a user-abort (or empty
// resumePicked) surfaced from PickVerifyTask returns ok=false
// and RunResume must exit cleanly with nil. The agent must never
// be invoked.
func TestRunResume_PickerCancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedResumableVerify(t, nil)
	seedResumableVerify(t, nil)
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
	if len(agent.verifiedReqs) != 0 {
		t.Fatal("agent should not be touched after cancel")
	}
}

// TestRunResume_AppliesDefaults exercises ResumeOptions.withDefaults.
func TestRunResume_AppliesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if err := RunResume(context.Background(), ResumeOptions{
		Agents: []codingagents.Agent{newScriptedAgent()},
	}); err != nil {
		t.Fatalf("RunResume: %v", err)
	}
}

// TestRunResume_ListUnavailable forces openTaskLog to fail for the
// list helper.
func TestRunResume_ListUnavailable(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jdir, "tasks"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := RunResume(context.Background(), ResumeOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "tasks db") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunResume_FromTaskUnavailable is symmetric to the list case.
func TestRunResume_FromTaskUnavailable(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jdir, "tasks"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: "anything",
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newScriptedAgent()},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "tasks db") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunResume_ListDecodeError plants a bad JSON payload in the
// tasks bucket so listResumableTasks returns a decode error.
func TestRunResume_ListDecodeError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(store.BucketTasks); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketTasks, "bad", "not-json"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
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
// under a known task id and exercises the resolveResumeByID branch
// that returns a non-fs.ErrNotExist error from GetTask.
func TestRunResume_FromTaskDecodeError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(store.BucketTasks); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketTasks, "broken", "not-json"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	err = RunResume(context.Background(), ResumeOptions{
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

// TestRunResume_StampsCompletedOnPass pins the lifecycle wiring on
// the resume path.
func TestRunResume_StampsCompletedOnPass(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, nil)
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}

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
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusCompleted {
		t.Fatalf("Status = %q, want completed (PASS verdict on resume)", tasks[0].Status)
	}
	if tasks[0].DoneAt == nil {
		t.Fatalf("DoneAt should be stamped: %+v", tasks[0])
	}
}

// TestRunResume_FailLeavesVerifyDone covers the no-retries branch
// on the resume path: the resumed verifier writes FAIL and the
// lifecycle finalises the task as verify-done (no re-loop on
// resume).
func TestRunResume_FailLeavesVerifyDone(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, nil)
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"FAIL"}

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
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusVerifyDone {
		t.Fatalf("Status = %q, want verify-done", tasks[0].Status)
	}
	if tasks[0].DoneAt != nil {
		t.Fatalf("DoneAt should remain nil: %v", tasks[0].DoneAt)
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
	if got := viper.GetString("verify.resume.from_task"); got != "abc" {
		t.Errorf("verify.resume.from_task = %q, want %q", got, "abc")
	}
}

func TestNewResumeCmd_FromTaskEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("VERIFY_RESUME_FROM_TASK", "env-task")

	_ = newResumeCmd()
	if got := viper.GetString("verify.resume.from_task"); got != "env-task" {
		t.Errorf("verify.resume.from_task = %q, want %q", got, "env-task")
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
}

// TestNewResumeCmd_RunEPropagates exercises the RunE closure.
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

// TestRunResume_RegisteredAsChild verifies `j verify resume` is
// wired as a child of `j verify`.
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
		t.Fatal("`j verify resume` should be registered as a child of `j verify`")
	}
}

// TestBeginVerifyTaskResume_PreservesCursorAndBegin pins the
// resume lifecycle helper directly.
func TestBeginVerifyTaskResume_PreservesCursorAndBegin(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, nil)
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	preBegin := existing.VerifyBeginAt
	preCursor := existing.VerifyResumeCursor

	lc := existing.BeginVerifyResume(io.Discard)
	lc.Finish(store.VerifyOutcomeNoRetries, nil)

	tasks := readTasks(t)
	if tasks[0].VerifyResumeCursor != preCursor {
		t.Fatalf("VerifyResumeCursor changed: got %q, want %q", tasks[0].VerifyResumeCursor, preCursor)
	}
	if tasks[0].VerifyBeginAt == nil || !tasks[0].VerifyBeginAt.Equal(*preBegin) {
		t.Fatalf("VerifyBeginAt = %v, want preserved %v", tasks[0].VerifyBeginAt, preBegin)
	}
}

// TestBeginVerifyTaskResume_NilBeginAtStampsFresh covers the
// fallback path when the existing task has no VerifyBeginAt.
func TestBeginVerifyTaskResume_NilBeginAtStampsFresh(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, func(task *store.Task) { task.VerifyBeginAt = nil })
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	lc := existing.BeginVerifyResume(io.Discard)
	lc.Finish(store.VerifyOutcomeNoRetries, nil)

	tasks := readTasks(t)
	if tasks[0].VerifyBeginAt == nil {
		t.Fatalf("VerifyBeginAt should be stamped: %+v", tasks)
	}
}

// TestRunResume_Verify_AlwaysInteractive pins the always-interactive
// contract for `j verify resume`: regardless of the verifier bucket's
// stored `interactive` value (or absence thereof), resume forces
// Interactive=true. Headless resume has no stdin path back to the
// human, so respecting a stored `interactive=false` would dead-end
// any clarification turn.
func TestRunResume_Verify_AlwaysInteractive(t *testing.T) {
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
				seedVerifierInteractive(t, tc.bucketValue)
			}
			id, _ := seedResumableVerify(t, nil)
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
			if len(agent.verifiedReqs) != 1 || !agent.verifiedReqs[0].Interactive {
				t.Fatalf("Interactive = false, want true (resume always forces interactive, bucket=%q): %+v", tc.bucketValue, agent.verifiedReqs)
			}
		})
	}
}

// TestRunResume_WaitsForSpawnedChild is the resume-flow analogue
// of TestRunVerifyLoop_WaitsForSpawnedChild. The verifier child
// sleeps before writing the findings file; without
// run.WaitForExit, RunResume would parseVerdict before the child
// has written PASS and finalise the row as verify-done.
func TestRunResume_WaitsForSpawnedChild(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, nil)
	// Drop the seeded findings so the test doesn't accidentally
	// read a stale "FAIL" if WaitForExit were a no-op; only the
	// freshly-spawned child's write should reach disk.
	if err := os.Remove(filepath.Join(mustTasksDir(t), id, store.VerifierFindingsFileName)); err != nil {
		t.Fatalf("remove findings: %v", err)
	}
	agent := &spawnVerifyAgent{
		verdicts: []string{"PASS"},
		sleepDur: "0.2",
	}
	var stdout bytes.Buffer
	start := time.Now()
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
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("RunResume returned in %v, expected to wait for the spawned child's 200ms sleep", elapsed)
	}
	if !strings.Contains(stdout.String(), "verify resume on task "+id) {
		t.Fatalf("stdout = %q, want resume line", stdout.String())
	}
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusCompleted {
		t.Fatalf("Status = %q, want completed (PASS verdict from spawned child)", tasks[0].Status)
	}
	findings := filepath.Join(mustTasksDir(t), id, store.VerifierFindingsFileName)
	data, readErr := os.ReadFile(findings)
	if readErr != nil {
		t.Fatalf("read findings: %v", readErr)
	}
	if !strings.Contains(string(data), "VERDICT: PASS") {
		t.Fatalf("findings = %q, want PASS verdict", string(data))
	}
}

// TestRunResume_VerifierWaitCtxCancelled covers the new
// run.WaitForExit branch in RunResume: the verifier returns a
// live PID, ctx is cancelled mid-poll, and the lifecycle finalises
// the row as `help`.
func TestRunResume_VerifierWaitCtxCancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, nil)
	pid := startLongChild(t)
	agent := &liveChildAgent{pid: pid}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := RunResume(ctx, ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusHelp {
		t.Fatalf("Status = %q, want help", tasks[0].Status)
	}
}

// TestRunResume_Verify_ForwardsMustRead pins AC: `j verify resume`
// loads the project's mustRead setting and threads it into
// VerifyRequest.MustRead so the resume turn inherits the same
// project-wide context the first run had. Without this,
// BuildVerifierResume would silently render a must-read-less prompt.
func TestRunResume_Verify_ForwardsMustRead(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedProjectMustRead(t, "AGENTS.md;CLAUDE.md")
	id, _ := seedResumableVerify(t, nil)
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
	if err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if len(agent.verifiedReqs) != 1 {
		t.Fatalf("verify calls = %d, want 1", len(agent.verifiedReqs))
	}
	want := []string{"AGENTS.md", "CLAUDE.md"}
	if got := agent.verifiedReqs[0].MustRead; len(got) != len(want) {
		t.Fatalf("MustRead = %v, want %v", got, want)
	} else {
		for i, v := range want {
			if got[i] != v {
				t.Fatalf("MustRead[%d] = %q, want %q (case must be preserved)", i, got[i], v)
			}
		}
	}
}

// TestRunResume_Verify_MustReadUnsetYieldsNil covers the
// no-bucket-entry branch of resolver.MustRead: when the project has
// no mustRead setting, the resume call must still proceed and pass a
// nil/empty slice (mirroring what the first-run verify flow does).
func TestRunResume_Verify_MustReadUnsetYieldsNil(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id, _ := seedResumableVerify(t, nil)
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
	if err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	}); err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if len(agent.verifiedReqs) != 1 {
		t.Fatalf("verify calls = %d, want 1", len(agent.verifiedReqs))
	}
	if len(agent.verifiedReqs[0].MustRead) != 0 {
		t.Fatalf("MustRead = %v, want empty when bucket has no entry", agent.verifiedReqs[0].MustRead)
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

// seedVerifierInteractive writes a literal `interactive` value into
// the verifier bucket. Reused by TestRunResume_Verify_AlwaysInteractive
// to prove the stored value is intentionally ignored on resume.
func seedVerifierInteractive(t *testing.T, value string) {
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
	if err := s.EnsureBucket(store.BucketVerifier); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	if err := s.Put(store.BucketVerifier, "interactive", value); err != nil {
		t.Fatalf("Put interactive: %v", err)
	}
}

// silence unused-import pseudo by referencing fmt in a dead path.
var _ = fmt.Sprintf
