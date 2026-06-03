package testcases_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle/orchestrator"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

func TestTaskToml_LegacyFlatCLICompatibility(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	showID := "legacy-show"
	seedLegacyFlatTask(t, showID, tasks.StatusPlanDone)
	stdout, _, err := testutil.RunCobra(t,
		clitasks.New(), "show", "--from-task", showID)
	if err != nil {
		t.Fatalf("tasks show legacy flat task.toml: %v", err)
	}
	if !strings.Contains(stdout, "plan_tool = \"cursor\"") {
		t.Fatalf("show stdout did not render legacy flat TOML:\n%s", stdout)
	}

	continueID := "legacy-continue"
	seedLegacyFlatTask(t, continueID, tasks.StatusPlanDone)
	continueArgv := filepath.Join(t.TempDir(), "continue-argv.txt")
	err = clitasks.RunContinue(t.Context(), clitasks.ContinueOptions{
		TaskID:  continueID,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{&legacyFlatAgent{}},
		JBinary: argvJStub(t, continueArgv),
	})
	if err != nil {
		t.Fatalf("continue legacy flat task: %v", err)
	}
	requireArgv(t, readArgv(t, continueArgv),
		"tasks", "orchestrate", "--id", continueID,
		"--phase=from-work", "--interactive=false")

	resumeID := "legacy-resume"
	seedLegacyFlatTask(t, resumeID, tasks.StatusWorking)
	resumeArgv := filepath.Join(t.TempDir(), "resume-argv.txt")
	ui := &legacyFlatUI{pickReturn: resumeID}
	err = clitasks.RunResumeWork(t.Context(), clitasks.ResumeWorkOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{&legacyFlatAgent{}},
		UI:      ui,
		JBinary: argvJStub(t, resumeArgv),
	})
	if err != nil {
		t.Fatalf("resume-work legacy flat task: %v", err)
	}
	if !ui.sawTask(resumeID) {
		t.Fatalf("resume picker rows did not include legacy task: %v", ui.rows)
	}
	requireArgv(t, readArgv(t, resumeArgv),
		"tasks", "orchestrate", "--id", resumeID,
		"--phase=work-only", "--interactive=true")

	transitionID := "legacy-transition"
	seedLegacyFlatTask(t, transitionID, tasks.StatusWorking)
	agent := &legacyFlatAgent{}
	err = clitasks.RunOrchestrate(t.Context(), clitasks.OrchestrateOptions{
		TaskID: transitionID,
		Phase:  orchestrator.RunPhaseWorkOnly,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
	})
	if err != nil {
		t.Fatalf("orchestrate work-only legacy flat task: %v", err)
	}
	if agent.workCalls.Load() != 1 {
		t.Fatalf("work calls = %d, want 1", agent.workCalls.Load())
	}
	row := readTaskRow(t, transitionID)
	if row.Status != tasks.StatusWorkDone || row.WorkResumeSession != "w-old" {
		t.Fatalf("transitioned row = %+v, want work-done with session", row)
	}
	if !strings.Contains(readTaskToml(t, transitionID), "[metadata]") {
		t.Fatalf("transition did not rewrite task.toml as sectioned")
	}
}

func seedLegacyFlatTask(
	t *testing.T,
	id string,
	status tasks.TaskStatus,
) {
	t.Helper()
	dir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	writeTaskArtifact(t, filepath.Join(dir, tasks.RequirementsFileName),
		"# legacy\n")
	writeTaskArtifact(t, filepath.Join(dir, tasks.PlanFileName),
		"1. keep compatibility\n")
	testutil.SeedRawTaskFile(t, id, []byte(legacyFlatTaskToml(id, status)))
}

func legacyFlatTaskToml(id string, status tasks.TaskStatus) string {
	return fmt.Sprintf(`id = "%s"
status = "%s"
plan_tool = "cursor"
plan_model = "sonnet-4"
work_tool = "cursor"
work_model = "sonnet-4"
verify_tool = "cursor"
verify_model = "sonnet-4"
worktree = "legacy-wt"
summary = "legacy flat task"
plan_resume_session = "p-old"
work_resume_session = "w-old"
verify_resume_session = "v-old"
agent_log_path = "/tmp/legacy-agent.log"
linear_issue = "SPA-124"
pull_request_url = "https://example.test/pull/124"
plan_begin_at = 2026-05-01T00:00:00Z
plan_end_at = 2026-05-01T00:10:00Z
work_begin_at = 2026-05-01T00:20:00Z
work_end_at = 0001-01-01T00:00:00Z
verify_begin_at = 0001-01-01T00:00:00Z
verify_end_at = 0001-01-01T00:00:00Z
done_at = 0001-01-01T00:00:00Z
`, id, status)
}

func writeTaskArtifact(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func requireArgv(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

type legacyFlatUI struct {
	pickReturn string
	rows       []tasks.Task
}

func (u *legacyFlatUI) ConfirmDiscard(
	context.Context,
	tasks.Task,
) (bool, error) {
	return false, nil
}

func (u *legacyFlatUI) PickTask(
	_ context.Context,
	rows []tasks.Task,
) (string, bool, error) {
	u.rows = append([]tasks.Task(nil), rows...)
	return u.pickReturn, u.pickReturn != "", nil
}

func (u *legacyFlatUI) sawTask(id string) bool {
	for _, row := range u.rows {
		if row.ID == id && row.WorkResumeSession == "w-old" {
			return true
		}
	}
	return false
}

type legacyFlatAgent struct {
	workCalls atomic.Int32
}

func (*legacyFlatAgent) Name() string { return "cursor" }

func (*legacyFlatAgent) ListModels(context.Context) ([]string, error) {
	return []string{"sonnet-4"}, nil
}

func (*legacyFlatAgent) CheckLogin(context.Context) error { return nil }

func (*legacyFlatAgent) NewResumeID(context.Context) (string, error) {
	return "fresh", nil
}

func (*legacyFlatAgent) Plan(
	context.Context,
	codingagents.PlanRequest,
) (int, error) {
	return 0, errors.New("planner should not run")
}

func (a *legacyFlatAgent) Work(
	context.Context,
	codingagents.WorkRequest,
) (int, error) {
	a.workCalls.Add(1)
	return 0, nil
}

func (*legacyFlatAgent) Verify(
	context.Context,
	codingagents.VerifyRequest,
) (int, error) {
	return 0, errors.New("verifier should not run")
}

func (*legacyFlatAgent) FormatLog(line []byte) []byte { return line }
