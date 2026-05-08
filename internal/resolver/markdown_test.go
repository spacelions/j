package resolver

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

func mustSetPlanApproval(t *testing.T, val bool) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(store.BucketProject); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketProject,
		store.KeyPlanRequiresApproval, strconv.FormatBool(val)); err != nil {
		t.Fatal(err)
	}
	s.Close()
}

type planAgent struct {
	resumeID string
	pid      int
	planErr  error
	reqBody  string
	planBody string
	lastReq  codingagents.PlanRequest
}

func (a *planAgent) Name() string { return "cursor" }
func (a *planAgent) ListModels(context.Context) ([]string, error) {
	return []string{"m"}, nil
}
func (a *planAgent) CheckLogin(context.Context) error { return nil }
func (a *planAgent) NewResumeID(context.Context) (string, error) {
	if a.resumeID == "ERR" {
		return "", errors.New("resume failed")
	}
	return a.resumeID, nil
}

func (a *planAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	a.lastReq = req
	if a.reqBody != "" {
		if err := os.WriteFile(req.RequirementsOutputPath, []byte(a.reqBody), 0o644); err != nil {
			return 0, err
		}
	}
	if a.planBody != "" {
		if err := os.WriteFile(req.PlanOutputPath, []byte(a.planBody), 0o644); err != nil {
			return 0, err
		}
	}
	return a.pid, a.planErr
}

func (a *planAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	return 0, errors.New("unused")
}

func (a *planAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("unused")
}

func TestResolvePlanMarkdown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.md")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	source, err := ResolvePlanMarkdown(path)
	if err != nil {
		t.Fatalf("ResolvePlanMarkdown: %v", err)
	}
	if source.Target != path || source.Body != "body" {
		t.Fatalf("source = %+v", source)
	}
	badDir := filepath.Join(dir, "dir.md")
	if err := os.Mkdir(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePlanMarkdown(badDir); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("dir err = %v", err)
	}
}

func TestRunPlanMarkdown(t *testing.T) {
	setupResolverProject(t)
	mustSetPlanApproval(t, false)
	sourcePath := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(sourcePath, []byte("source body"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &planAgent{resumeID: "resume", reqBody: "refined", planBody: "plan"}
	var stdout bytes.Buffer
	if err := RunPlanMarkdown(t.Context(), PlanMarkdownOptions{
		RawTarget:   sourcePath,
		Stdout:      &stdout,
		Stderr:      &bytes.Buffer{},
		Agent:       agent,
		Model:       "m",
		Interactive: true,
	}); err != nil {
		t.Fatalf("RunPlanMarkdown: %v", err)
	}
	if agent.lastReq.FromFilePath != sourcePath || !agent.lastReq.Interactive {
		t.Fatalf("last request = %+v", agent.lastReq)
	}
	if !strings.Contains(stdout.String(), "requirements.md and plan.md") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	rows, err := ListAllTasks()
	if err != nil || len(rows) != 1 || rows[0].Status != tasks.StatusPlanDone {
		t.Fatalf("tasks = %+v, %v", rows, err)
	}
}

func TestRunPlanMarkdownPlanError(t *testing.T) {
	setupResolverProject(t)
	agent := &planAgent{planErr: errors.New("plan failed")}
	err := RunPlanMarkdown(t.Context(), PlanMarkdownOptions{
		Source: PlanMarkdownSource{Target: "/tmp/source.md", Body: "body"},
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Agent:  agent,
		Model:  "m",
	})
	if err == nil || !strings.Contains(err.Error(), "plan failed") {
		t.Fatalf("plan err = %v", err)
	}
}

func TestRunPlanMarkdownWarningsAndBackground(t *testing.T) {
	setupResolverProject(t)
	agent := &planAgent{resumeID: "ERR", pid: os.Getpid()}
	var stdout, stderr bytes.Buffer
	err := RunPlanMarkdown(t.Context(), PlanMarkdownOptions{
		Source: PlanMarkdownSource{Target: "/tmp/source.md", Body: "body"},
		Stdout: &stdout,
		Stderr: &stderr,
		Agent:  agent,
		Model:  "m",
	})
	if err != nil {
		t.Fatalf("RunPlanMarkdown background: %v", err)
	}
	if !strings.Contains(stdout.String(), "running in background") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "resume failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestStartTargetFiles(t *testing.T) {
	setupResolverProject(t)
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := NewStartTargetFromMarkdown(path)
	if err != nil {
		t.Fatalf("NewStartTargetFromMarkdown: %v", err)
	}
	logPath, err := PrepareStartTaskFiles(target)
	if err != nil {
		t.Fatalf("PrepareStartTaskFiles: %v", err)
	}
	if filepath.Base(logPath) != tasks.AgentLogFileName {
		t.Fatalf("log path = %q", logPath)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(logPath), tasks.RequirementsFileName))
	if err != nil || string(data) != "body" {
		t.Fatalf("requirements = %q, %v", string(data), err)
	}
	logPath, err = PrepareStartTaskFiles(StartTarget{TaskID: "existing"})
	if err != nil || filepath.Base(logPath) != tasks.AgentLogFileName {
		t.Fatalf("existing log path = %q, %v", logPath, err)
	}
}

// TestRunPlanFromBody covers the in-memory-body path used by
// `j plan --from-linear`: requirements.md is staged from the body
// before agent.Plan runs, the agent's FromFilePath points at the
// staged file, and the lifecycle finishes without printing the
// markdown-target line (the body source has no on-disk path).
func TestRunPlanFromBody(t *testing.T) {
	setupResolverProject(t)
	agent := &planAgent{resumeID: "resume", planBody: "plan body"}
	var stdout bytes.Buffer
	body := "# from linear\n\nbody text\n\n---\nLinear: https://x\n"
	if err := RunPlanFromBody(t.Context(), PlanMarkdownOptions{
		Stdout:      &stdout,
		Stderr:      &bytes.Buffer{},
		Agent:       agent,
		Model:       "m",
		Interactive: true,
	}, body, "linear:ENG-1", "ENG-1"); err != nil {
		t.Fatalf("RunPlanFromBody: %v", err)
	}
	if !strings.Contains(agent.lastReq.FromFilePath, "requirements.md") {
		t.Fatalf("FromFilePath = %q, want staged requirements.md", agent.lastReq.FromFilePath)
	}
	staged, err := os.ReadFile(agent.lastReq.FromFilePath)
	if err != nil {
		t.Fatalf("read staged: %v", err)
	}
	if string(staged) != body {
		t.Fatalf("staged body = %q, want %q", staged, body)
	}
	if !strings.Contains(stdout.String(), "requirements.md and plan.md") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

// TestRunPlanFromBody_DefaultSourceLabel pins the source-label
// fallback: an empty sourceLabel is replaced by the staged path so
// the recorded source is never blank.
func TestRunPlanFromBody_DefaultSourceLabel(t *testing.T) {
	setupResolverProject(t)
	agent := &planAgent{resumeID: "resume", planBody: "plan"}
	if err := RunPlanFromBody(t.Context(), PlanMarkdownOptions{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Agent:  agent,
		Model:  "m",
	}, "body", "", ""); err != nil {
		t.Fatalf("RunPlanFromBody: %v", err)
	}
}

func TestStartTargetErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := NewStartTargetFromMarkdown("missing.md"); err == nil {
		t.Fatal("NewStartTargetFromMarkdown error = nil")
	}
	if _, err := PrepareStartTaskFiles(StartTarget{TaskID: "new", IsNew: true, Body: "body"}); err == nil {
		t.Fatal("PrepareStartTaskFiles error = nil")
	}
}
