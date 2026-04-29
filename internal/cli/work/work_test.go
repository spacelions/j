package work

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/huh"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// testCursorChatID is the `cursor-agent create-chat` id from the
// TestMain stub; Run stores it in Task.WorkResumeCursor for the
// Cursor backend.
const testCursorChatID = "00000000-0000-4000-8000-000000000001"

// TestMain chdir's the entire work-package test binary into an
// ephemeral directory so any test that calls Run without an explicit
// Store doesn't pollute the source tree with a `.j/settings` file
// when withDefaults lazily opens the default DB. It prepends a
// `cursor-agent` stub for `create-chat` so Run stays hermetic.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "work-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	if err := os.Chdir(tmp); err != nil {
		panic(err)
	}
	stubDir, err := os.MkdirTemp(tmp, "cursor-path")
	if err != nil {
		panic(err)
	}
	stub := filepath.Join(stubDir, "cursor-agent")
	stubScript := `#!/bin/sh
if [ "$1" = "create-chat" ]; then
  echo "00000000-0000-4000-8000-000000000001"
  exit 0
fi
echo "cursor-agent test stub: unhandled argv" >&2
exit 1
`
	if err := os.WriteFile(stub, []byte(stubScript), 0o755); err != nil {
		panic(err)
	}
	if err := os.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// openTestStore returns a fresh *store.Store rooted in t.TempDir() with
// the coder bucket pre-created.
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.EnsureBucket(store.BucketCoder); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// mustInit lays down the .j layout in the current working directory.
// Tests that previously relied on lazy creation by Run / OpenDefault
// / EnsureTaskDir must call this helper after t.Chdir so the new
// pre-init contract is satisfied. Idempotent.
func mustInit(t *testing.T) {
	t.Helper()
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
}

func mustGet(t *testing.T, s *store.Store, key string) (string, bool) {
	t.Helper()
	v, ok, err := s.Get(store.BucketCoder, key)
	if err != nil {
		t.Fatalf("Get %s: %v", key, err)
	}
	return v, ok
}

// scriptedUI returns predetermined answers for each prompt and tracks
// how many times each prompt was invoked.
type scriptedUI struct {
	fromFile     string
	pickedID     string
	resumePicked string
	tool         string
	model        string
	askErr       error
	pickErr      error
	resumeErr    error
	toolErr      error
	modelErr     error

	askCalls         int
	pickCalls        int
	pickResumeCalls  int
	toolCalls        int
	modelCalls       int

	pickedTasks       []store.Task
	pickResumedTasks  []store.Task
}

func (s *scriptedUI) AskFromFile(context.Context) (string, error) {
	s.askCalls++
	if s.askErr != nil {
		return "", s.askErr
	}
	return s.fromFile, nil
}

func (s *scriptedUI) PickPlanTask(_ context.Context, tasks []store.Task) (string, error) {
	s.pickCalls++
	s.pickedTasks = tasks
	if s.pickErr != nil {
		return "", s.pickErr
	}
	if s.pickedID != "" {
		return s.pickedID, nil
	}
	if len(tasks) == 0 {
		return "", errors.New("scriptedUI: no tasks to pick")
	}
	return tasks[0].ID, nil
}

func (s *scriptedUI) PickWorkTask(_ context.Context, tasks []store.Task) (string, error) {
	s.pickResumeCalls++
	s.pickResumedTasks = tasks
	if s.resumeErr != nil {
		return "", s.resumeErr
	}
	if s.resumePicked != "" {
		return s.resumePicked, nil
	}
	if len(tasks) == 0 {
		return "", errors.New("scriptedUI: no tasks to resume-pick")
	}
	return tasks[0].ID, nil
}

func (s *scriptedUI) SelectTool(_ context.Context, options []string) (string, error) {
	s.toolCalls++
	if s.toolErr != nil {
		return "", s.toolErr
	}
	if s.tool != "" {
		return s.tool, nil
	}
	return options[0], nil
}

func (s *scriptedUI) SelectModel(_ context.Context, options []string) (string, error) {
	s.modelCalls++
	if s.modelErr != nil {
		return "", s.modelErr
	}
	if s.model != "" {
		return s.model, nil
	}
	return options[0], nil
}

// scriptedAgent stands in for any codingagents.Agent in tests. Plan is
// implemented because the Agent interface requires it; work_test never
// invokes it.
type scriptedAgent struct {
	name      string
	models    []string
	modelsErr error
	loginErr  error
	resumeID  string
	resumeErr error
	workErr   error

	listed     int
	checked    int
	worked     int
	resumeIDed int
	lastReq    codingagents.WorkRequest
}

func newScriptedAgent() *scriptedAgent {
	return &scriptedAgent{
		name:     "cursor",
		models:   []string{"sonnet-4", "gpt-5"},
		resumeID: testCursorChatID,
	}
}

func (s *scriptedAgent) Name() string { return s.name }

func (s *scriptedAgent) ListModels(context.Context) ([]string, error) {
	s.listed++
	if s.modelsErr != nil {
		return nil, s.modelsErr
	}
	return s.models, nil
}

func (s *scriptedAgent) CheckLogin(context.Context) error {
	s.checked++
	return s.loginErr
}

func (s *scriptedAgent) NewResumeID(context.Context) (string, error) {
	s.resumeIDed++
	if s.resumeErr != nil {
		return "", s.resumeErr
	}
	return s.resumeID, nil
}

func (s *scriptedAgent) Plan(context.Context, codingagents.PlanRequest) error {
	return errors.New("scriptedAgent: Plan should not be called from work tests")
}

func (s *scriptedAgent) Work(_ context.Context, req codingagents.WorkRequest) error {
	s.worked++
	s.lastReq = req
	return s.workErr
}

// taskFilePath returns the absolute path of a body file (e.g.
// store.PlanFileName) for an existing task id under the current
// working directory's `.j/tasks/<id>/`. It mirrors the production
// `filepath.Join(DefaultTasksDir(), id, name)` recipe so test
// assertions stay aligned with the on-disk layout contract.
func taskFilePath(t *testing.T, id, name string) string {
	t.Helper()
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	return filepath.Join(tasksDir, id, name)
}

// seedPlanDoneTask creates a `plan-done` task row in bbolt and writes
// the corresponding `.j/tasks/<id>/plan.md` and `requirements.md`
// files. The id is returned so callers can reference it via
// Options.TaskID. Use after t.Chdir(t.TempDir()).
func seedPlanDoneTask(t *testing.T, summary, planBody, requirementBody string) string {
	t.Helper()
	id := store.NewTaskID()
	if _, err := store.EnsureTaskDir(id); err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	planPath := taskFilePath(t, id, store.PlanFileName)
	if err := os.WriteFile(planPath, []byte(planBody), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if requirementBody != "" {
		reqPath := taskFilePath(t, id, store.RequirementsFileName)
		if err := os.WriteFile(reqPath, []byte(requirementBody), 0o644); err != nil {
			t.Fatalf("write requirements: %v", err)
		}
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
	begin := time.Now().UTC().Add(-time.Hour)
	end := begin.Add(time.Minute)
	task := store.Task{
		ID:               id,
		Status:           store.StatusPlanDone,
		InvokedTool:      "cursor",
		InvokedModel:     "sonnet-4",
		PlanResumeCursor: "seed-plan-cursor",
		Summary:          summary,
		PlanBeginAt:      &begin,
		PlanEndAt:        &end,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}

func writePlan(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestRun_ByTaskID_Success exercises the bbolt-sourced reuse path:
// `--from-task <id>` loads the existing row, executes its plan.md, and
// updates the same row to `work-done`.
func TestRun_ByTaskID_Success(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "seeded summary", "1. step\n2. step", "# requirement\nbody")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		TaskID:      id,
		Interactive: boolPtr(true),
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.askCalls != 0 || ui.pickCalls != 0 {
		t.Fatalf("UI should be silent: ask=%d pick=%d", ui.askCalls, ui.pickCalls)
	}
	if agent.worked != 1 {
		t.Fatalf("agent.Work calls = %d, want 1", agent.worked)
	}
	if !strings.Contains(stdout.String(), "coding against task "+id) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("expected one task row (reuse): %+v", tasks)
	}
	got := tasks[0]
	if got.ID != id {
		t.Fatalf("task id = %q, want %q", got.ID, id)
	}
	if got.Status != store.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
	if got.PlanResumeCursor != "seed-plan-cursor" {
		t.Fatalf("PlanResumeCursor = %q, want seed-plan-cursor", got.PlanResumeCursor)
	}
	if got.WorkResumeCursor != testCursorChatID {
		t.Fatalf("WorkResumeCursor = %q, want %q", got.WorkResumeCursor, testCursorChatID)
	}
	if got.PlanBeginAt == nil || got.PlanEndAt == nil {
		t.Fatalf("plan timestamps lost on reuse: %+v", got)
	}
	if got.WorkBeginAt == nil || got.WorkEndAt == nil {
		t.Fatalf("work timestamps not set: %+v", got)
	}
	if got.DoneAt != nil {
		t.Fatalf("DoneAt should remain nil after work-done: %v", got.DoneAt)
	}
}

// TestRun_ByTaskID_NotFound surfaces a clear error when the requested
// id does not exist in bbolt.
func TestRun_ByTaskID_NotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	// Open the tasks log just to materialize the empty bucket.
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
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
	_ = s.Close()
	agent := newScriptedAgent()
	err = Run(context.Background(), Options{
		TaskID: "missing-id",
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), `task "missing-id" not found`) {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_ByTaskID_StatusRejected covers validateForWork: a task that
// is not plan-done or help should be rejected.
func TestRun_ByTaskID_StatusRejected(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "plan", "")
	// Mutate the task to working so reuse is rejected.
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	got.Status = store.StatusWorking
	if err := s.PutTask(got); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	agent := newScriptedAgent()
	err = Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "already working") {
		t.Fatalf("err = %v", err)
	}
	if agent.worked != 0 {
		t.Fatal("agent.Work should not run when validateForWork rejects")
	}
}

// TestRun_AutoPicksLatestPlanDone exercises the no-flag path with a
// single plan-done row in bbolt: Run should reuse it without
// prompting the user.
func TestRun_AutoPicksLatestPlanDone(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "auto", "auto plan", "")
	agent := newScriptedAgent()
	ui := &scriptedUI{}

	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.askCalls != 0 || ui.pickCalls != 0 {
		t.Fatalf("UI should be silent for single-task auto-pick: ask=%d pick=%d", ui.askCalls, ui.pickCalls)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].ID != id || tasks[0].Status != store.StatusWorkDone {
		t.Fatalf("tasks = %+v", tasks)
	}
}

// TestRun_PickerOverMultipleTasks verifies the UI picker is invoked
// when more than one plan-done task is available.
func TestRun_PickerOverMultipleTasks(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id1 := seedPlanDoneTask(t, "first", "plan one", "")
	id2 := seedPlanDoneTask(t, "second", "plan two", "")
	agent := newScriptedAgent()
	ui := &scriptedUI{pickedID: id2}

	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickPlanTask = %d, want 1", ui.pickCalls)
	}
	gotIDs := make([]string, len(ui.pickedTasks))
	for i, x := range ui.pickedTasks {
		gotIDs[i] = x.ID
	}
	wantIDs := []string{id2, id1} // most recent first via SortTasks
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("picker tasks = %v, want %v", gotIDs, wantIDs)
	}
	tasks := readTasks(t)
	for _, task := range tasks {
		if task.ID == id2 && task.Status != store.StatusWorkDone {
			t.Fatalf("picked task should be work-done: %+v", task)
		}
		if task.ID == id1 && task.Status != store.StatusPlanDone {
			t.Fatalf("unpicked task should stay plan-done: %+v", task)
		}
	}
}

// TestRun_PickerError surfaces the UI picker error verbatim.
func TestRun_PickerError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedPlanDoneTask(t, "a", "x", "")
	seedPlanDoneTask(t, "b", "x", "")
	agent := newScriptedAgent()
	ui := &scriptedUI{pickErr: errors.New("picker boom")}

	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "picker boom") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_NoPlanDoneFallsBackToAskFromFile pins the empty-bbolt path:
// when no plan-done task exists, Run prompts AskFromFile.
func TestRun_NoPlanDoneFallsBackToAskFromFile(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	plan := writePlan(t, "legacy plan body")
	agent := newScriptedAgent()
	ui := &scriptedUI{fromFile: plan}

	err := Run(context.Background(), Options{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.askCalls != 1 {
		t.Fatalf("AskFromFile = %d, want 1", ui.askCalls)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusWorkDone {
		t.Fatalf("tasks = %+v, want one work-done task from legacy import", tasks)
	}
}

// TestRun_FromFile_LegacyImport exercises the legacy file path:
// passing --from-file imports the file into a new .j/tasks/<id>/
// folder and creates a fresh task row.
func TestRun_FromFile_LegacyImport(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	dir := t.TempDir()
	planSrc := filepath.Join(dir, "spec.plan.md")
	if err := os.WriteFile(planSrc, []byte("# legacy plan\nstep"), 0o600); err != nil {
		t.Fatal(err)
	}
	reqSrc := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(reqSrc, []byte("# legacy requirement\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		FromFile: planSrc,
		Stdout:   &stdout,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout.String(), "coding against ") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("expected one new task row, got %+v", tasks)
	}
	got := tasks[0]
	if got.Status != store.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
	if got.Summary != "legacy requirement" {
		t.Fatalf("Summary = %q, want sidecar heading", got.Summary)
	}
	planPath := taskFilePath(t, got.ID, store.PlanFileName)
	if data, err := os.ReadFile(planPath); err != nil {
		t.Fatalf("read imported plan: %v", err)
	} else if !strings.Contains(string(data), "legacy plan") {
		t.Fatalf("imported plan = %q", string(data))
	}
	reqPath := taskFilePath(t, got.ID, store.RequirementsFileName)
	if data, err := os.ReadFile(reqPath); err != nil {
		t.Fatalf("read imported requirements: %v", err)
	} else if !strings.Contains(string(data), "legacy requirement") {
		t.Fatalf("imported requirements = %q", string(data))
	}
}

// TestRun_FromFile_NoSidecar covers the legacy file path when there
// is no `<stem>.md` sidecar; the imported task gets only plan.md.
func TestRun_FromFile_NoSidecar(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	plan := writePlan(t, "## plan only")
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		FromFile: plan,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d", len(tasks))
	}
	if tasks[0].Summary != "plan only" {
		t.Fatalf("Summary = %q, want plan-body fallback", tasks[0].Summary)
	}
}

func TestRun_Headless_PropagatesFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "x", "")
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		TaskID:      id,
		Interactive: boolPtr(false),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if agent.lastReq.Interactive {
		t.Fatalf("Interactive should be false: %+v", agent.lastReq)
	}
}

func TestRun_AskFromFileError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	agent := newScriptedAgent()
	ui := &scriptedUI{askErr: errors.New("ask boom")}
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "ask boom") {
		t.Fatalf("err = %v", err)
	}
	if agent.listed != 0 {
		t.Fatal("agent should not be invoked when AskFromFile errored")
	}
}

func TestRun_FromFile_ValidationError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "spec.txt")
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		FromFile: bad,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "not a markdown") {
		t.Fatalf("err = %v", err)
	}
	if agent.worked != 0 {
		t.Fatal("agent.Work should not have been invoked")
	}
}

func TestRun_FromFile_PlanReadError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	plan := writePlan(t, "x")
	if err := os.Chmod(plan, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(plan, 0o600) })

	err := Run(context.Background(), Options{
		FromFile: plan,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newScriptedAgent()},
		UI:       &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "read plan") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_NoAgents(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "x", "")
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected error when no agents are configured")
	}
}

// TestRun_NoAgents_AppliesDefaults exercises the nil-defaulting branches
// in Options.withDefaults by passing a fully zero Options and relying on
// Run to short-circuit on the empty agent list before any UI is touched.
func TestRun_NoAgents_AppliesDefaults(t *testing.T) {
	err := Run(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_ListModelsError_StopsBeforeUI(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "x", "")
	agent := newScriptedAgent()
	agent.modelsErr = errors.New("network down")

	ui := &scriptedUI{}
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if ui.modelCalls != 0 {
		t.Fatalf("SelectModel called despite list error: %d", ui.modelCalls)
	}
	if agent.checked != 0 || agent.worked != 0 {
		t.Fatal("login/work should not have been invoked")
	}
}

func TestRun_SelectModelError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "x", "")
	agent := newScriptedAgent()
	ui := &scriptedUI{modelErr: errors.New("model boom")}
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "model boom") {
		t.Fatalf("err = %v", err)
	}
	if agent.checked != 0 {
		t.Fatal("CheckLogin should not be invoked when SelectModel errored")
	}
}

func TestRun_LoginFailure_StopsBeforeAgent(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "x", "")
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")

	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
	if agent.worked != 0 {
		t.Fatal("agent.Work should not have been invoked")
	}
}

// TestRun_UICancelled exercises the user-abort path: when a huh
// prompt returns huh.ErrUserAborted, Run treats it as a clean exit
// (nil error) and never reaches the agent. The "cancelled by user"
// message previously surfaced via an ErrCancelled sentinel is gone
// by design — abort is silent.
func TestRun_UICancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "x", "")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{toolErr: huh.ErrUserAborted},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil (abort exits cleanly)", err)
	}
	if agent.listed != 0 || agent.worked != 0 {
		t.Fatal("agent should not be touched after cancel")
	}
}

func TestRun_AgentWorkError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "x", "")
	agent := newScriptedAgent()
	agent.workErr = errors.New("agent boom")

	var stdout bytes.Buffer
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "agent boom") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stdout.String(), "coding against") {
		t.Fatalf("stdout should not announce success on Work error: %q", stdout.String())
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusHelp {
		t.Fatalf("tasks = %+v, want one help task", tasks)
	}
}

func TestRun_NewResumeID_ErrorWarnsButContinues(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "x", "")
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
	if agent.lastReq.ResumeChatID != "" {
		t.Fatalf("ResumeChatID should be empty after NewResumeID error: %q", agent.lastReq.ResumeChatID)
	}
}

func TestRun_UnknownToolFromUI(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "x", "")
	agent := newScriptedAgent()
	agent.name = "cursor"

	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{tool: "codex"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_PersistsCoderSelection drives a successful work run with a
// real *store.Store and asserts the coder bucket holds tool/model/
// interactive only — the work source (plan path) must stay
// unpersisted so the user is prompted for it every run.
func TestRun_PersistsCoderSelection(t *testing.T) {
	s := openTestStore(t)
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		TaskID:      id,
		Interactive: boolPtr(true),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          &scriptedUI{},
		Store:       s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := map[string]string{
		"tool":        "cursor",
		"model":       "sonnet-4",
		"interactive": "true",
	}
	for k, v := range want {
		got, ok := mustGet(t, s, k)
		if !ok || got != v {
			t.Fatalf("coder.%s = %q (ok=%v), want %q", k, got, ok, v)
		}
	}
	for _, forbidden := range []string{"target", "source", "plan", "from_file", "task"} {
		if _, ok := mustGet(t, s, forbidden); ok {
			t.Fatalf("coder.%s should not be persisted", forbidden)
		}
	}
}

// TestRun_LoginFailure_DoesNotPersist confirms the coder bucket is
// untouched when login fails (we only persist after agentpick.Pick
// returns successfully).
func TestRun_LoginFailure_DoesNotPersist(t *testing.T) {
	s := openTestStore(t)
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")

	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
		Store:  s,
	})
	if err == nil {
		t.Fatal("expected login error")
	}
	entries, listErr := s.List(store.BucketCoder)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(entries) != 0 {
		t.Fatalf("coder bucket should be empty: %v", entries)
	}
}

// TestRun_SelectionCancelled_DoesNotPersist mirrors the login-failure
// case for the user-cancel path through agentpick.Pick. With the
// abort-to-nil contract, Run returns no error on cancel; the
// invariant the test guards is that nothing was persisted to the
// coder bucket because Pick was never confirmed.
func TestRun_SelectionCancelled_DoesNotPersist(t *testing.T) {
	s := openTestStore(t)
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{toolErr: huh.ErrUserAborted},
		Store:  s,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil (abort exits cleanly)", err)
	}
	entries, listErr := s.List(store.BucketCoder)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(entries) != 0 {
		t.Fatalf("coder bucket should be empty: %v", entries)
	}
}

// TestRun_StoreWriteError_WarnsAndContinues exercises the persistence
// best-effort branch: a closed store returns errors from Put, and the
// agent must still run.
func TestRun_StoreWriteError_WarnsAndContinues(t *testing.T) {
	s := openTestStore(t)
	id := seedPlanDoneTask(t, "x", "body", "")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
		Store:  s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: persist") {
		t.Fatalf("stderr = %q, want warning", stderr.String())
	}
	if agent.worked != 1 {
		t.Fatal("agent.Work should still have been invoked despite persist error")
	}
}

// TestRun_FromSettings_PopulatedStore_SkipsPrompts is the work
// counterpart of the plan test: a populated coder bucket must
// short-circuit the prompts and route the stored model into the
// WorkRequest. The bucket is left untouched (no "from_settings"
// key, no rewrite).
func TestRun_FromSettings_PopulatedStore_SkipsPrompts(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "gpt-5"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "interactive", "true"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		TaskID:       id,
		Interactive:  boolPtr(true),
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           ui,
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.toolCalls != 0 || ui.modelCalls != 0 {
		t.Fatalf("UI prompts should be skipped: tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if agent.listed != 0 {
		t.Fatalf("ListModels should not be called: got %d", agent.listed)
	}
	if agent.checked != 1 {
		t.Fatalf("CheckLogin = %d, want 1", agent.checked)
	}
	if agent.lastReq.Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", agent.lastReq.Model)
	}
	entries, err := s.List(store.BucketCoder)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := make([]string, len(entries))
	for i, kv := range entries {
		got[i] = kv.Key
	}
	want := []string{"interactive", "model", "tool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("coder keys = %v, want %v", got, want)
	}
	if strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should not warn when store is populated: %q", stderr.String())
	}
}

func TestRun_FromSettings_EmptyStore_FallsBackToPrompt(t *testing.T) {
	s := openTestStore(t)
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		TaskID:       id,
		Interactive:  boolPtr(true),
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           ui,
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.toolCalls != 1 || ui.modelCalls != 1 {
		t.Fatalf("UI should be prompted: tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should warn about fallback: %q", stderr.String())
	}
	if v, ok := mustGet(t, s, "tool"); !ok || v != "cursor" {
		t.Fatalf("coder.tool = %q (ok=%v), want cursor", v, ok)
	}
}

func TestRun_FromSettings_False_AlwaysPrompts(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()
	ui := &scriptedUI{}
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		TaskID:       id,
		FromSettings: false,
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           ui,
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.toolCalls != 1 || ui.modelCalls != 1 {
		t.Fatalf("UI should be prompted: tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should not warn on explicit --from-settings=false: %q", stderr.String())
	}
}

func TestRun_FromSettings_LoginFailureSurfaces(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()
	agent.loginErr = errors.New("not logged in")

	err := Run(context.Background(), Options{
		TaskID:       id,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
	if agent.worked != 0 {
		t.Fatal("agent.Work should not run when CheckLogin fails on FromStore path")
	}
}

func TestRun_FromSettings_NonSentinelStoreError(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "ghost"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()
	ui := &scriptedUI{}

	err := Run(context.Background(), Options{
		TaskID:       id,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           ui,
		Store:        s,
	})
	if err == nil || !strings.Contains(err.Error(), `unknown tool "ghost"`) {
		t.Fatalf("err = %v", err)
	}
	if ui.toolCalls != 0 {
		t.Fatal("Pick should not be invoked on non-sentinel error")
	}
}

// TestRun_StoreLazyDefault confirms a nil opts.Store causes
// withDefaults to open and close the default DB and write to the
// coder bucket.
func TestRun_StoreLazyDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()

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
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	got, ok, err := s.Get(store.BucketCoder, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != "cursor" {
		t.Fatalf("coder.tool = %q (ok=%v)", got, ok)
	}
}

// TestPersistCoderSelection_NilStore exercises the early-return branch
// when no Store is configured.
func TestPersistCoderSelection_NilStore(t *testing.T) {
	var stderr bytes.Buffer
	persistCoderSelection(Options{Stderr: &stderr}, "cursor", "sonnet-4")
	if stderr.Len() != 0 {
		t.Fatalf("stderr should stay empty, got %q", stderr.String())
	}
}

// TestRun_ByTaskID_TasksDBUnavailable forces store.OpenTaskLog to
// return ok=false by parking a regular file at .j/tasks (the legacy
// schema). resolveByTaskID then must surface a clean error.
func TestRun_ByTaskID_TasksDBUnavailable(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jdir, "tasks"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		TaskID: "anything",
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "tasks db unavailable") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_ByTaskID_PlanReadError covers resolveByTaskID's read-plan
// error branch: the bbolt row exists but the plan.md file was deleted
// out from under it.
func TestRun_ByTaskID_PlanReadError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "plan body", "")
	planPath := taskFilePath(t, id, store.PlanFileName)
	if err := os.Remove(planPath); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "read plan") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_ListPlanDoneTasks_DBUnavailable ensures the auto-pick path
// surfaces a clean error when the tasks DB cannot be opened (legacy
// .j/tasks file blocks the new directory layout).
func TestRun_ListPlanDoneTasks_DBUnavailable(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jdir, "tasks"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "tasks db unavailable") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_ListPlanDoneTasks_DecodeError plants a bad JSON payload in
// the tasks bucket so ListTasks returns an error; resolvePlan must
// propagate it instead of swallowing it.
func TestRun_ListPlanDoneTasks_DecodeError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
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

	agent := newScriptedAgent()
	err = Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "decode task") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_FromFile_EnsureTaskDirError covers the resolveFromFile
// branch where store.EnsureTaskDir errors out (legacy regular file at
// .j/tasks blocks creating new task subdirs).
func TestRun_FromFile_EnsureTaskDirError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jdir, "tasks"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := writePlan(t, "x")
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		FromFile: plan,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "ensure task dir") {
		t.Fatalf("err = %v", err)
	}
}

// TestOpenLifecycle_PutTaskErrorWarns drives the put-error branch
// inside openLifecycle by handing it a Task with an empty ID, which
// store.PutTask rejects without ever reaching bbolt.
func TestOpenLifecycle_PutTaskErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	lc := beginWorkTaskNew(Options{Stderr: &stderr}, &scriptedAgent{name: "cursor"}, "m", "", "/tmp/x.plan.md", "", "body", "")
	if lc.store == nil {
		t.Fatal("store should be open even when initial put fails")
	}
	t.Cleanup(func() { lc.finishWork(nil) })
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestValidateForWork covers every validateForWork branch.
func TestValidateForWork(t *testing.T) {
	cases := []struct {
		status  store.TaskStatus
		wantErr string
	}{
		{store.StatusPlanDone, ""},
		{store.StatusHelp, ""},
		{store.StatusPlanning, "still planning"},
		{store.StatusWorking, "already working"},
		{store.StatusWorkDone, "already past work"},
		{store.StatusVerifying, "already past work"},
		{store.StatusVerifyDone, "already past work"},
		{store.StatusCompleted, "already past work"},
		{store.TaskStatus("nonsense"), "unsupported status"},
	}
	for _, c := range cases {
		err := validateForWork(store.Task{ID: "x", Status: c.status})
		if c.wantErr == "" && err != nil {
			t.Errorf("status=%q: unexpected error %v", c.status, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("status=%q: err = %v, want %q", c.status, err, c.wantErr)
		}
	}
}

// TestRun_FromSettings_StoredInteractiveFalseOverridesDefault pins
// the bug fix: with FromSettings on and no explicit Interactive
// pointer, a stored interactive=false in the coder bucket flows
// through to both the agent request and the persisted value.
func TestRun_FromSettings_StoredInteractiveFalseOverridesDefault(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "interactive", "false"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		TaskID:       id,
		Interactive:  nil,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if agent.lastReq.Interactive {
		t.Fatalf("agent.lastReq.Interactive = true, want false (stored override): %+v", agent.lastReq)
	}
	if v, ok := mustGet(t, s, "interactive"); !ok || v != "false" {
		t.Fatalf("coder.interactive = %q (ok=%v), want false", v, ok)
	}
}

// TestRun_FromSettings_ExplicitInteractiveWins covers the
// explicit-beats-stored half of the precedence: a non-nil
// Interactive pointer must win even when the bucket says otherwise.
func TestRun_FromSettings_ExplicitInteractiveWins(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "interactive", "false"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		TaskID:       id,
		Interactive:  boolPtr(true),
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("agent.lastReq.Interactive = false, want true (explicit wins): %+v", agent.lastReq)
	}
}

// TestRun_FromSettings_StoredInteractiveUnparseable confirms a
// garbled bucket value is treated as "not set" and the cobra
// default flows through without a warning.
func TestRun_FromSettings_StoredInteractiveUnparseable(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "interactive", "garbage"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		TaskID:       id,
		Interactive:  nil,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("agent.lastReq.Interactive = false, want true (cobra default): %+v", agent.lastReq)
	}
	if strings.Contains(stderr.String(), "interactive") {
		t.Fatalf("stderr should not warn on unparseable interactive: %q", stderr.String())
	}
}

// TestRun_FromSettings_False_IgnoresStoredInteractive pins the
// FromSettings=false branch of resolveInteractive: the bucket is
// never consulted, the explicit value flows through.
func TestRun_FromSettings_False_IgnoresStoredInteractive(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "interactive", "false"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		TaskID:       id,
		Interactive:  boolPtr(true),
		FromSettings: false,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("agent.lastReq.Interactive = false, want true: %+v", agent.lastReq)
	}
}

// TestRun_FromSettings_NoInteractiveKey_DefaultTrue locks down the
// resolveInteractive default branch: a populated bucket without an
// `interactive` entry leaves the cobra default (true) intact.
func TestRun_FromSettings_NoInteractiveKey_DefaultTrue(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketCoder, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(store.BucketCoder, "model", "sonnet-4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()

	err := Run(context.Background(), Options{
		TaskID:       id,
		Interactive:  nil,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
		Store:        s,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !agent.lastReq.Interactive {
		t.Fatalf("agent.lastReq.Interactive = false, want true (default): %+v", agent.lastReq)
	}
}
