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

	"github.com/charmbracelet/huh"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// testCursorChatID is the `cursor-agent create-chat` id from the
// TestMain stub; Run stores it in Task.VerifyResumeCursor for the
// Cursor backend.
const testCursorChatID = "00000000-0000-4000-8000-000000000099"

// TestMain chdir's the entire verify-package test binary into an
// ephemeral directory so any test that calls Run without an
// explicit Store doesn't pollute the source tree with a
// `.j/settings` file when withDefaults lazily opens the default
// DB. It prepends a `cursor-agent` stub for `create-chat` so Run
// stays hermetic.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "verify-test-*")
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
  echo "00000000-0000-4000-8000-000000000099"
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

func mustInit(t *testing.T) {
	t.Helper()
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
}

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
	if err := s.EnsureBucket(store.BucketVerifier); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustGet(t *testing.T, s *store.Store, key string) (string, bool) {
	t.Helper()
	v, ok, err := s.Get(store.BucketVerifier, key)
	if err != nil {
		t.Fatalf("Get %s: %v", key, err)
	}
	return v, ok
}

// readTasks lists every task in the per-cwd tasks DB. Tests call
// this after Run to assert the lifecycle wrote what we expect.
func readTasks(t *testing.T) []store.Task {
	t.Helper()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	return got
}

// scriptedUI returns predetermined answers for each prompt and
// tracks how many times each prompt was invoked. Mirrors
// work.scriptedUI's shape.
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

	askCalls        int
	pickCalls       int
	pickResumeCalls int
	toolCalls       int
	modelCalls      int

	pickedTasks      []store.Task
	pickResumedTasks []store.Task
}

func (s *scriptedUI) AskFromFile(context.Context) (string, error) {
	s.askCalls++
	if s.askErr != nil {
		return "", s.askErr
	}
	return s.fromFile, nil
}

func (s *scriptedUI) PickWorkDoneTask(_ context.Context, tasks []store.Task) (string, error) {
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

func (s *scriptedUI) PickVerifyTask(_ context.Context, tasks []store.Task) (string, error) {
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

// scriptedAgent stands in for any codingagents.Agent. Plan/Verify/
// Work each track invocations independently so tests can drive
// PASS-on-first vs FAIL-then-PASS vs error scenarios.
type scriptedAgent struct {
	name      string
	models    []string
	modelsErr error
	loginErr  error
	resumeID  string
	resumeErr error

	// verifyHook and workHook receive each request and decide what
	// to do with the on-disk findings file. The default verifyHook
	// writes the supplied verifyVerdicts entry verbatim to
	// VerifierFindingsOutputPath; the default workHook is a no-op.
	verifyVerdicts []string
	verifyErrAt    int
	verifyErr      error
	workErrAt      int
	workErr        error
	workPID        int

	listed       int
	checked      int
	resumeIDed   int
	verifiedReqs []codingagents.VerifyRequest
	workedReqs   []codingagents.WorkRequest
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

func (s *scriptedAgent) Plan(context.Context, codingagents.PlanRequest) (int, error) {
	return 0, errors.New("scriptedAgent: Plan should not be called from verify tests")
}

func (s *scriptedAgent) Work(_ context.Context, req codingagents.WorkRequest) (int, error) {
	idx := len(s.workedReqs)
	s.workedReqs = append(s.workedReqs, req)
	if s.workErr != nil && idx == s.workErrAt {
		return 0, s.workErr
	}
	return s.workPID, nil
}

func (s *scriptedAgent) Verify(_ context.Context, req codingagents.VerifyRequest) (int, error) {
	idx := len(s.verifiedReqs)
	s.verifiedReqs = append(s.verifiedReqs, req)
	if s.verifyErr != nil && idx == s.verifyErrAt {
		return 0, s.verifyErr
	}
	verdict := "PASS"
	if idx < len(s.verifyVerdicts) {
		verdict = s.verifyVerdicts[idx]
	}
	if req.VerifierFindingsOutputPath != "" {
		body := fmt.Sprintf("- iteration %d findings\nVERDICT: %s\n", idx, verdict)
		if err := os.WriteFile(req.VerifierFindingsOutputPath, []byte(body), 0o644); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

// taskFilePath returns the absolute path of a body file (e.g.
// store.PlanFileName) for an existing task id under the current
// working directory's `.j/tasks/<id>/`.
func taskFilePath(t *testing.T, id, name string) string {
	t.Helper()
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	return filepath.Join(tasksDir, id, name)
}

// seedWorkDoneTask creates a `work-done` task row in bbolt and
// writes the corresponding `.j/tasks/<id>/{plan,requirements}.md`
// files. Use after t.Chdir(t.TempDir()).
func seedWorkDoneTask(t *testing.T, summary, planBody, requirementBody string) string {
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
	planBegin := time.Now().UTC().Add(-2 * time.Hour)
	planEnd := planBegin.Add(time.Minute)
	workBegin := planEnd.Add(time.Minute)
	workEnd := workBegin.Add(time.Minute)
	task := store.Task{
		ID:               id,
		Status:           store.StatusWorkDone,
		InvokedTool:      "cursor",
		InvokedModel:     "sonnet-4",
		PlanResumeCursor: "seed-plan-cursor",
		WorkResumeCursor: "seed-work-cursor",
		Summary:          summary,
		PlanBeginAt:      &planBegin,
		PlanEndAt:        &planEnd,
		WorkBeginAt:      &workBegin,
		WorkEndAt:        &workEnd,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}

// TestRun_PassOnFirstIteration pins the happy path: the verifier
// writes VERDICT: PASS on its first turn, the orchestrator
// finalises the task as `completed` with DoneAt stamped, and the
// coder is never invoked.
func TestRun_PassOnFirstIteration(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "summary", "plan body", "# req\nbody")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		TaskID:        id,
		Interactive:   boolPtr(true),
		MaxIterations: 3,
		Stdout:        &stdout,
		Stderr:        io.Discard,
		Agents:        []codingagents.Agent{agent},
		UI:            &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(agent.verifiedReqs) != 1 {
		t.Fatalf("verify calls = %d, want 1", len(agent.verifiedReqs))
	}
	if len(agent.workedReqs) != 0 {
		t.Fatalf("work calls = %d, want 0 on PASS", len(agent.workedReqs))
	}
	if !strings.Contains(stdout.String(), "verified task "+id) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusCompleted {
		t.Fatalf("tasks = %+v", tasks)
	}
	got := tasks[0]
	if got.DoneAt == nil {
		t.Fatalf("DoneAt should be stamped on completed: %+v", got)
	}
	if got.VerifyBeginAt == nil || got.VerifyEndAt == nil {
		t.Fatalf("verify timestamps missing: %+v", got)
	}
	if got.VerifyResumeCursor != testCursorChatID {
		t.Fatalf("VerifyResumeCursor = %q", got.VerifyResumeCursor)
	}
	findings := taskFilePath(t, id, store.VerifierFindingsFileName)
	if data, err := os.ReadFile(findings); err != nil {
		t.Fatalf("read findings: %v", err)
	} else if !strings.Contains(string(data), "VERDICT: PASS") {
		t.Fatalf("findings = %q", string(data))
	}
}

// TestRun_FailThenPass exercises the bounded loop convergence: the
// first verifier turn returns FAIL, the coder fix turn runs with
// the findings populated, and the second verifier turn returns
// PASS. The task ends as `completed`.
func TestRun_FailThenPass(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "summary", "plan body", "# req")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"FAIL", "PASS"}

	err := Run(context.Background(), Options{
		TaskID:        id,
		Interactive:   boolPtr(true),
		MaxIterations: 3,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
		Agents:        []codingagents.Agent{agent},
		UI:            &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(agent.verifiedReqs) != 2 {
		t.Fatalf("verify calls = %d, want 2", len(agent.verifiedReqs))
	}
	if len(agent.workedReqs) != 1 {
		t.Fatalf("work calls = %d, want 1", len(agent.workedReqs))
	}
	work := agent.workedReqs[0]
	if !work.Resume {
		t.Fatalf("coder fix turn should set Resume=true: %+v", work)
	}
	if !strings.Contains(work.FixFindings, "VERDICT: FAIL") {
		t.Fatalf("FixFindings missing FAIL line: %q", work.FixFindings)
	}
	if work.ResumeChatID != "seed-work-cursor" {
		t.Fatalf("coder fix turn should reuse seeded WorkResumeCursor, got %q", work.ResumeChatID)
	}
	// The second verify turn must Resume so the previous
	// verifier session is reused.
	if !agent.verifiedReqs[1].Resume {
		t.Fatalf("second verify turn should set Resume=true: %+v", agent.verifiedReqs[1])
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusCompleted {
		t.Fatalf("tasks = %+v", tasks)
	}
}

// TestRun_LoopExhausted pins the no-retries terminal state: every
// verifier turn returns FAIL and the loop runs out, finalising the
// task as `verify-done` (DoneAt stays nil).
func TestRun_LoopExhausted(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "summary", "plan body", "# req")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"FAIL", "FAIL"}
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		TaskID:        id,
		Interactive:   boolPtr(true),
		MaxIterations: 2,
		Stdout:        &stdout,
		Stderr:        io.Discard,
		Agents:        []codingagents.Agent{agent},
		UI:            &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(agent.verifiedReqs) != 2 {
		t.Fatalf("verify calls = %d, want 2", len(agent.verifiedReqs))
	}
	if len(agent.workedReqs) != 1 {
		t.Fatalf("work calls = %d, want 1 (between the two verify turns)", len(agent.workedReqs))
	}
	if !strings.Contains(stdout.String(), "exhausted retries") {
		t.Fatalf("stdout should mention exhausted retries: %q", stdout.String())
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusVerifyDone {
		t.Fatalf("tasks = %+v, want one verify-done", tasks)
	}
	if tasks[0].DoneAt != nil {
		t.Fatalf("DoneAt must remain nil on verify-done: %v", tasks[0].DoneAt)
	}
}

// TestRun_MaxIterations1 disables the loop: the verifier runs once
// and a FAIL terminates the task as verify-done without invoking
// the coder.
func TestRun_MaxIterations1(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "summary", "plan body", "# req")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"FAIL"}

	err := Run(context.Background(), Options{
		TaskID:        id,
		Interactive:   boolPtr(true),
		MaxIterations: 1,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
		Agents:        []codingagents.Agent{agent},
		UI:            &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(agent.verifiedReqs) != 1 {
		t.Fatalf("verify calls = %d, want 1", len(agent.verifiedReqs))
	}
	if len(agent.workedReqs) != 0 {
		t.Fatalf("work calls = %d, want 0 with max-iterations=1", len(agent.workedReqs))
	}
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusVerifyDone {
		t.Fatalf("Status = %q", tasks[0].Status)
	}
}

// TestRun_VerifierError surfaces an error returned by Agent.Verify
// and finalises the task as `help`.
func TestRun_VerifierError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan body", "")
	agent := newScriptedAgent()
	agent.verifyErr = errors.New("verify boom")

	err := Run(context.Background(), Options{
		TaskID:        id,
		Interactive:   boolPtr(true),
		MaxIterations: 3,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
		Agents:        []codingagents.Agent{agent},
		UI:            &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "verify boom") {
		t.Fatalf("err = %v", err)
	}
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusHelp {
		t.Fatalf("Status = %q, want help", tasks[0].Status)
	}
}

// TestRun_CoderFixError exercises the error path during the coder
// fix turn: verify returns FAIL, coder errors out, the lifecycle
// records `help`.
func TestRun_CoderFixError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan body", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"FAIL", "PASS"}
	agent.workErr = errors.New("coder boom")

	err := Run(context.Background(), Options{
		TaskID:        id,
		Interactive:   boolPtr(true),
		MaxIterations: 3,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
		Agents:        []codingagents.Agent{agent},
		UI:            &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "coder boom") {
		t.Fatalf("err = %v", err)
	}
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusHelp {
		t.Fatalf("Status = %q, want help", tasks[0].Status)
	}
}

// TestRun_MalformedVerdictTreatedAsFail pins parseVerdict's
// fall-through branch: a finding file whose terminal line is not
// the literal verdict line should be treated as FAIL. With
// MaxIterations=1 this terminates as verify-done.
func TestRun_MalformedVerdictTreatedAsFail(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan body", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"weird verdict"}

	err := Run(context.Background(), Options{
		TaskID:        id,
		Interactive:   boolPtr(true),
		MaxIterations: 1,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
		Agents:        []codingagents.Agent{agent},
		UI:            &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tasks := readTasks(t)
	if tasks[0].Status != store.StatusVerifyDone {
		t.Fatalf("Status = %q", tasks[0].Status)
	}
}

// TestParseVerdict_EdgeCases pins every parseVerdict branch on a
// table.
func TestParseVerdict_EdgeCases(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body string
		want string
	}{
		{"missing-file", "", "FAIL"},
		{"empty", "", "FAIL"},
		{"only-whitespace", "\n\n   \n", "FAIL"},
		{"plain-pass", "VERDICT: PASS", "PASS"},
		{"plain-fail", "VERDICT: FAIL", "FAIL"},
		{"surrounding-whitespace", "  VERDICT:   PASS   \n", "PASS"},
		{"trailing-blank-lines", "VERDICT: PASS\n\n\n", "PASS"},
		{"crlf", "VERDICT: PASS\r\n", "PASS"},
		{"mixed-case", "verdict: pass\n", "PASS"},
		{"prose-then-verdict", "issues: none\nVERDICT: PASS\n", "PASS"},
		{"prose-after-verdict", "VERDICT: PASS\nbut actually no\n", "FAIL"},
		{"unknown-marker", "VERDICT: maybe\n", "FAIL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, c.name+".md")
			if c.name == "missing-file" {
				if got := parseVerdict(path); got != c.want {
					t.Fatalf("parseVerdict(missing) = %q, want %q", got, c.want)
				}
				return
			}
			if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := parseVerdict(path); got != c.want {
				t.Fatalf("parseVerdict(%s) = %q, want %q (body=%q)", c.name, got, c.want, c.body)
			}
		})
	}
}

// TestValidateForVerify covers every validateForVerify branch.
func TestValidateForVerify(t *testing.T) {
	cases := []struct {
		status  store.TaskStatus
		wantErr string
	}{
		{store.StatusWorkDone, ""},
		{store.StatusVerifyDone, ""},
		{store.StatusHelp, ""},
		{store.StatusPlanning, "still planning"},
		{store.StatusPlanDone, "not been worked"},
		{store.StatusWorking, "still working"},
		{store.StatusVerifying, "already verifying"},
		{store.StatusCompleted, "already completed"},
		{store.TaskStatus("nonsense"), "unsupported status"},
	}
	for _, c := range cases {
		err := validateForVerify(store.Task{ID: "x", Status: c.status})
		if c.wantErr == "" && err != nil {
			t.Errorf("status=%q: unexpected error %v", c.status, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("status=%q: err = %v, want %q", c.status, err, c.wantErr)
		}
	}
}

// TestRun_NoAgents short-circuits before touching anything.
func TestRun_NoAgents(t *testing.T) {
	err := Run(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_NoCandidatesError exercises resolveTask's empty-list
// branch.
func TestRun_NoCandidatesError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "no work-done tasks") {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_FromTask_NotFound pins resolveByTaskID's not-found path.
func TestRun_FromTask_NotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
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

// TestRun_FromTask_StatusRejected validates the orchestrator
// surfaces the right error when the task is in an inactive
// non-allowlisted state.
func TestRun_FromTask_StatusRejected(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "body", "")
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
	got.Status = store.StatusCompleted
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
	if err == nil || !strings.Contains(err.Error(), "already completed") {
		t.Fatalf("err = %v", err)
	}
	if len(agent.verifiedReqs) != 0 {
		t.Fatal("agent.Verify should not run when validateForVerify rejects")
	}
}

// TestRun_AutoPicksLatestWorkDone with a single work-done task
// must skip the picker.
func TestRun_AutoPicksLatestWorkDone(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
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
	if ui.pickCalls != 0 {
		t.Fatalf("PickWorkDoneTask = %d, want 0 for single-task auto-pick", ui.pickCalls)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].ID != id || tasks[0].Status != store.StatusCompleted {
		t.Fatalf("tasks = %+v", tasks)
	}
}

// TestRun_PickerOverMultipleTasks pins multi-candidate picker.
func TestRun_PickerOverMultipleTasks(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id1 := seedWorkDoneTask(t, "first", "plan one", "")
	id2 := seedWorkDoneTask(t, "second", "plan two", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
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
		t.Fatalf("PickWorkDoneTask = %d, want 1", ui.pickCalls)
	}
	gotIDs := make([]string, len(ui.pickedTasks))
	for i, x := range ui.pickedTasks {
		gotIDs[i] = x.ID
	}
	want := map[string]bool{id1: true, id2: true}
	for _, id := range gotIDs {
		if !want[id] {
			t.Fatalf("picker tasks contain unexpected id %q (want %v)", id, want)
		}
	}
	tasks := readTasks(t)
	for _, task := range tasks {
		if task.ID == id2 && task.Status != store.StatusCompleted {
			t.Fatalf("picked task should be completed: %+v", task)
		}
		if task.ID == id1 && task.Status != store.StatusWorkDone {
			t.Fatalf("unpicked task should stay work-done: %+v", task)
		}
	}
}

// TestRun_PickerError surfaces the UI picker error verbatim.
func TestRun_PickerError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	seedWorkDoneTask(t, "a", "x", "")
	seedWorkDoneTask(t, "b", "x", "")
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

// TestRun_UICancelled exercises the user-abort path: when a huh
// prompt returns huh.ErrUserAborted, Run treats it as a clean exit.
func TestRun_UICancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
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
	if len(agent.verifiedReqs) != 0 {
		t.Fatal("agent.Verify should not be touched after cancel")
	}
}

// TestRun_NewResumeID_ErrorWarnsButContinues mirrors the work
// equivalent: a NewResumeID failure surfaces as a warning and the
// orchestrator runs the verifier without a resume id.
func TestRun_NewResumeID_ErrorWarnsButContinues(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.resumeErr = errors.New("create-chat down")
	agent.verifyVerdicts = []string{"PASS"}
	var stderr bytes.Buffer

	err := Run(context.Background(), Options{
		TaskID:        id,
		MaxIterations: 1,
		Stdout:        io.Discard,
		Stderr:        &stderr,
		Agents:        []codingagents.Agent{agent},
		UI:            &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "create-chat down") {
		t.Fatalf("stderr should warn: %q", stderr.String())
	}
	if agent.verifiedReqs[0].ResumeChatID != "" {
		t.Fatalf("ResumeChatID should be empty after NewResumeID error: %q", agent.verifiedReqs[0].ResumeChatID)
	}
}

// TestRun_UnknownToolFromUI rejects an off-list tool name.
func TestRun_UnknownToolFromUI(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
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

// TestRun_UnknownTool_OnTaskRow covers the lookupResumeAgent
// failure when the task's recorded InvokedTool no longer matches an
// available agent.
func TestRun_UnknownTool_OnTaskRow(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
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
	got.InvokedTool = "ghost"
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
	if err == nil || !strings.Contains(err.Error(), `unknown tool "ghost"`) {
		t.Fatalf("err = %v", err)
	}
}

// TestRun_PersistsVerifierSelection drives a successful verify run
// with a real *store.Store and asserts the verifier bucket holds
// tool/model/interactive only.
func TestRun_PersistsVerifierSelection(t *testing.T) {
	s := openTestStore(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}

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
			t.Fatalf("verifier.%s = %q (ok=%v), want %q", k, got, ok, v)
		}
	}
}

// TestRun_FromSettings_PopulatedStore_SkipsPrompts mirrors the
// work flow's settings-on path.
func TestRun_FromSettings_PopulatedStore_SkipsPrompts(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketVerifier, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "model", "gpt-5"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "interactive", "true"); err != nil {
		t.Fatal(err)
	}
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
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
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.toolCalls != 0 || ui.modelCalls != 0 {
		t.Fatalf("UI prompts should be skipped: tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if agent.listed != 0 {
		t.Fatalf("ListModels should not be called: got %d", agent.listed)
	}
	if agent.verifiedReqs[0].Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", agent.verifiedReqs[0].Model)
	}
}

// TestRun_FromSettings_EmptyStore_FallsBack covers the fallback to
// Pick when the bucket is empty.
func TestRun_FromSettings_EmptyStore_FallsBack(t *testing.T) {
	s := openTestStore(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
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
		t.Fatalf("stderr should fall back to prompt: %q", stderr.String())
	}
}

// TestRun_FromSettings_NonSentinelStoreError pins agentpick.FromStore's
// non-sentinel error path: a stored tool that isn't in the agent
// list.
func TestRun_FromSettings_NonSentinelStoreError(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketVerifier, "tool", "ghost"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "model", "sonnet-4"); err != nil {
		t.Fatal(err)
	}
	id := seedWorkDoneTask(t, "x", "plan", "")
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
		t.Fatalf("Pick should not be invoked on non-sentinel error: %d", ui.toolCalls)
	}
}

// TestRun_FromSettings_StoredInteractiveFalseOverridesDefault pins
// the precedence: stored=false propagates to the agent request and
// the persisted value when no explicit Interactive pointer is set.
func TestRun_FromSettings_StoredInteractiveFalseOverridesDefault(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketVerifier, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "model", "sonnet-4"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "interactive", "false"); err != nil {
		t.Fatal(err)
	}
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}

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
	if agent.verifiedReqs[0].Interactive {
		t.Fatalf("Interactive = true, want false")
	}
}

// TestRun_FromSettings_ExplicitWins mirrors work coverage.
func TestRun_FromSettings_ExplicitWins(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketVerifier, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "model", "sonnet-4"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "interactive", "false"); err != nil {
		t.Fatal(err)
	}
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}

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
	if !agent.verifiedReqs[0].Interactive {
		t.Fatalf("Interactive should be true (explicit wins)")
	}
}

// TestRun_FromSettings_StoredInteractiveUnparseable confirms a
// garbled bucket value leaves the cobra default true intact.
func TestRun_FromSettings_StoredInteractiveUnparseable(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketVerifier, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "model", "sonnet-4"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "interactive", "garbage"); err != nil {
		t.Fatal(err)
	}
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}

	err := Run(context.Background(), Options{
		TaskID:       id,
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
	if !agent.verifiedReqs[0].Interactive {
		t.Fatalf("Interactive should default to true on garbled stored value")
	}
}

// TestRun_FromSettings_False_IgnoresStored exercises the
// FromSettings=false branch.
func TestRun_FromSettings_False_IgnoresStored(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketVerifier, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "model", "sonnet-4"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "interactive", "false"); err != nil {
		t.Fatal(err)
	}
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}

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
	if !agent.verifiedReqs[0].Interactive {
		t.Fatalf("Interactive should be true")
	}
}

// TestRun_FromSettings_NoInteractiveKey_DefaultTrue covers the
// cobra default branch.
func TestRun_FromSettings_NoInteractiveKey_DefaultTrue(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put(store.BucketVerifier, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketVerifier, "model", "sonnet-4"); err != nil {
		t.Fatal(err)
	}
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}

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
	if !agent.verifiedReqs[0].Interactive {
		t.Fatalf("Interactive should default to true")
	}
}

// TestRun_LoginFailure_StopsBeforeAgent covers the CheckLogin
// branch.
func TestRun_LoginFailure_StopsBeforeAgent(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
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
	if len(agent.verifiedReqs) != 0 {
		t.Fatal("agent.Verify should not run when CheckLogin fails")
	}
}

// TestRun_ListModelsError_StopsBeforeUI covers the ListModels
// failure branch.
func TestRun_ListModelsError_StopsBeforeUI(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
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
}

// TestRun_ByTaskID_PlanReadError covers resolveByTaskID's
// read-plan error branch.
func TestRun_ByTaskID_PlanReadError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	if err := os.Remove(taskFilePath(t, id, store.PlanFileName)); err != nil {
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

// TestRun_ByTaskID_TasksDBUnavailable forces openTaskLog to fail.
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

// TestRun_List_DBUnavailable forces listVerifiableTasks to fail.
func TestRun_List_DBUnavailable(t *testing.T) {
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

// TestRun_List_DecodeError pins the bbolt decode-error branch.
func TestRun_List_DecodeError(t *testing.T) {
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

// TestPersistVerifierSelection_NilStore_LazyOpenSucceeds exercises
// the nil-Store branch when openSettingsStore can lay hands on a
// real `<cwd>/.j/settings`.
func TestPersistVerifierSelection_NilStore_LazyOpenSucceeds(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	var stderr bytes.Buffer
	persistVerifierSelection(Options{
		Stderr:      &stderr,
		Interactive: boolPtr(true),
	}, "cursor", "sonnet-4")
	if stderr.Len() != 0 {
		t.Fatalf("stderr should stay empty on success, got %q", stderr.String())
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	v, ok, err := s.Get(store.BucketVerifier, "tool")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || v != "cursor" {
		t.Fatalf("verifier.tool = %q (ok=%v), want cursor", v, ok)
	}
}

// TestPersistVerifierSelection_NilStore_LazyOpenFails covers the
// early-return branch when openSettingsStore can't open the DB.
func TestPersistVerifierSelection_NilStore_LazyOpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	var stderr bytes.Buffer
	persistVerifierSelection(Options{Stderr: &stderr}, "cursor", "sonnet-4")
	if !strings.Contains(stderr.String(), "warning: settings") {
		t.Fatalf("stderr = %q, want settings warning", stderr.String())
	}
}

// TestStoredVerifierInteractive_NilStore_LazyOpenFails covers
// the nil-Store + open-fails branch.
func TestStoredVerifierInteractive_NilStore_LazyOpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	var stderr bytes.Buffer
	v, ok := storedVerifierInteractive(Options{Stderr: &stderr})
	if ok || v {
		t.Fatalf("storedVerifierInteractive = (%v, %v), want (false, false)", v, ok)
	}
}

// TestRun_FromSettings_NilStore_LazyOpenSucceeds drives the
// nil-Store + populated-settings branch of verifierFromSettings:
// the helper opens `<cwd>/.j/settings`, reads the bucket, and
// surfaces the recorded tool/model so the UI prompts are skipped
// and storedVerifierInteractive returns the stored false.
func TestRun_FromSettings_NilStore_LazyOpenSucceeds(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	settingsPath, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := store.Open(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Put(store.BucketVerifier, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Put(store.BucketVerifier, "model", "gpt-5"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Put(store.BucketVerifier, "interactive", "false"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
	ui := &scriptedUI{}
	var stderr bytes.Buffer

	err = Run(context.Background(), Options{
		TaskID:       id,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.toolCalls != 0 || ui.modelCalls != 0 {
		t.Fatalf("UI prompts should be skipped on lazy-open success: tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if agent.verifiedReqs[0].Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", agent.verifiedReqs[0].Model)
	}
	if agent.verifiedReqs[0].Interactive {
		t.Fatalf("Interactive = true, want false (stored)")
	}
}

// TestStoredVerifierInteractive_NilStore_LazyOpenSucceeds covers
// the success branch where openSettingsStore lays hands on a real
// `<cwd>/.j/settings` and returns the recorded interactive flag.
func TestStoredVerifierInteractive_NilStore_LazyOpenSucceeds(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Put(store.BucketVerifier, "interactive", "false"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	v, ok := storedVerifierInteractive(Options{Stderr: io.Discard})
	if !ok || v {
		t.Fatalf("storedVerifierInteractive = (%v, %v), want (false, true)", v, ok)
	}
}

// TestRun_FromSettings_NilStore_SettingsOpenFails covers the lazy
// open-fails branches on the settings path.
func TestRun_FromSettings_NilStore_SettingsOpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	settingsPath, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settingsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
	var stderr bytes.Buffer

	err = Run(context.Background(), Options{
		TaskID:       id,
		FromSettings: true,
		Stdout:       io.Discard,
		Stderr:       &stderr,
		Agents:       []codingagents.Agent{agent},
		UI:           &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: settings") {
		t.Fatalf("stderr should warn about settings open: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should fall back to prompt: %q", stderr.String())
	}
}
