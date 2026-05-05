package tasks

import (
	"bytes"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// spawnSleepingChild forks a `sleep 10` and returns its PID so a
// reaper test can plug it in as a "live BackgroundPID". The
// cleanup kills the child after the test so leaks do not pile up
// in CI. The duration is long enough for the reaper poll to
// observe the child as alive without making the test slow.
func spawnSleepingChild(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// openTestStore returns a tasks-mode *Store rooted in t.TempDir().
// The store is closed by t.Cleanup; tests that need the underlying
// path call DefaultTasksDir after.
func openTestStore(t *testing.T) *tasks.Store {
	t.Helper()
	t.Chdir(t.TempDir())
	testutil.Init(t)
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedTaskDir creates `<cwd>/.j/tasks/<id>/` plus optional
// requirements.md / plan.md inside it. Returns the dir path so tests
// can use it for subsequent assertions.
func seedTaskDir(t *testing.T, id, requirements, plan string) string {
	t.Helper()
	dir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if requirements != "" {
		if err := os.WriteFile(filepath.Join(dir, tasks.RequirementsFileName), []byte(requirements), 0o644); err != nil {
			t.Fatalf("write requirements: %v", err)
		}
	}
	if plan != "" {
		if err := os.WriteFile(filepath.Join(dir, tasks.PlanFileName), []byte(plan), 0o644); err != nil {
			t.Fatalf("write plan: %v", err)
		}
	}
	return dir
}

// TestReap_LivePIDLeftAlone exercises the alive-child branch: the
// PID belongs to a real running process, so reapBackgroundTasks
// must NOT mutate the row.
func TestReap_LivePIDLeftAlone(t *testing.T) {
	s := openTestStore(t)
	pid := spawnSleepingChild(t)
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	begin := time.Now().UTC().Add(-time.Minute)
	in := []tasks.Task{{
		ID:            "live-task",
		Status:        tasks.StatusPlanning,
		BackgroundPID: pid,
		PlanBeginAt:   &begin,
		Summary:       "alive",
		AgentLogPath:  filepath.Join(tasksDir, "live-task", "agent.log"),
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	if len(out) != 1 {
		t.Fatalf("len(out) = %d", len(out))
	}
	got := out[0]
	if got.Status != tasks.StatusPlanning {
		t.Fatalf("Status = %q, want planning", got.Status)
	}
	if got.BackgroundPID != pid {
		t.Fatalf("BackgroundPID cleared on live row: %d", got.BackgroundPID)
	}
	if got.PlanEndAt != nil {
		t.Fatalf("PlanEndAt should remain nil on live row: %v", got.PlanEndAt)
	}
}

// TestReap_DeadPlanning_WithArtifacts exercises the plan-done branch:
// the PID is dead and both requirements.md + plan.md exist on disk.
// The row flips to plan-done, summary refreshes from requirements,
// PlanEndAt is stamped, and BackgroundPID is cleared.
func TestReap_DeadPlanning_WithArtifacts(t *testing.T) {
	s := openTestStore(t)
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	id := "done-with-artifacts"
	seedTaskDir(t, id, "# refined heading\nbody", "1. step\n2. step")
	begin := time.Now().UTC().Add(-time.Minute)
	in := []tasks.Task{{
		ID:            id,
		Status:        tasks.StatusPlanning,
		BackgroundPID: deadPID(t),
		PlanBeginAt:   &begin,
		Summary:       "stale",
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0", got.BackgroundPID)
	}
	if got.PlanEndAt == nil {
		t.Fatalf("PlanEndAt should be stamped")
	}
	if got.Summary != "refined heading" {
		t.Fatalf("Summary = %q, want %q", got.Summary, "refined heading")
	}
	persisted, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if persisted.Status != tasks.StatusPlanDone {
		t.Fatalf("persisted Status = %q", persisted.Status)
	}
}

// TestReap_DeadPlanning_NoArtifacts pins the help-status branch: the
// PID is dead but neither requirements nor plan made it to disk
// (e.g. the spawned child crashed early), so the row flips to help.
func TestReap_DeadPlanning_NoArtifacts(t *testing.T) {
	s := openTestStore(t)
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	id := "dead-without-artifacts"
	seedTaskDir(t, id, "", "")
	in := []tasks.Task{{
		ID:            id,
		Status:        tasks.StatusPlanning,
		BackgroundPID: deadPID(t),
		Summary:       "fallback",
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help", got.Status)
	}
	if got.PlanEndAt == nil {
		t.Fatalf("PlanEndAt should be stamped on help transition")
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0", got.BackgroundPID)
	}
	if got.Summary != "fallback" {
		t.Fatalf("Summary should not be overwritten when artifacts are missing: %q", got.Summary)
	}
}

// TestReap_DeadPlanning_OnlyPlanMissing exercises the artifact gate
// asymmetry: requirements.md exists but plan.md does not. The row
// must flip to help (both files are required for plan-done).
func TestReap_DeadPlanning_OnlyPlanMissing(t *testing.T) {
	s := openTestStore(t)
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	id := "dead-missing-plan"
	seedTaskDir(t, id, "# heading\nbody", "")
	in := []tasks.Task{{
		ID:            id,
		Status:        tasks.StatusPlanning,
		BackgroundPID: deadPID(t),
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	if out[0].Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help", out[0].Status)
	}
}

// TestReap_DeadWorking pins the work-done branch: a dead PID on a
// working row flips to work-done, stamps WorkEndAt, and clears
// BackgroundPID. There is no artifact gate.
func TestReap_DeadWorking(t *testing.T) {
	s := openTestStore(t)
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	id := "dead-working"
	seedTaskDir(t, id, "", "")
	begin := time.Now().UTC().Add(-time.Minute)
	in := []tasks.Task{{
		ID:            id,
		Status:        tasks.StatusWorking,
		BackgroundPID: deadPID(t),
		WorkBeginAt:   &begin,
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0", got.BackgroundPID)
	}
	if got.WorkEndAt == nil {
		t.Fatalf("WorkEndAt should be stamped")
	}
}

// TestReap_NonActiveStateUntouched pins the in-flight allowlist:
// rows in non-active statuses are ignored even when BackgroundPID
// is set. Only planning and working transition through the reaper.
func TestReap_NonActiveStateUntouched(t *testing.T) {
	s := openTestStore(t)
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	in := []tasks.Task{{
		ID:            "stale-help",
		Status:        tasks.StatusHelp,
		BackgroundPID: deadPID(t),
		Summary:       "old",
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help (untouched)", got.Status)
	}
	if got.BackgroundPID == 0 {
		t.Fatal("BackgroundPID should not be cleared for non-active rows")
	}
}

// TestReap_ZeroPIDUntouched covers the BackgroundPID == 0 short-circuit:
// foreground (interactive) and resume rows have no spawned child to
// reap, so the helper returns them verbatim without doing IsAlive
// or stat work.
func TestReap_ZeroPIDUntouched(t *testing.T) {
	s := openTestStore(t)
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	in := []tasks.Task{{
		ID:     "no-bg",
		Status: tasks.StatusPlanning,
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusPlanning {
		t.Fatalf("Status = %q, want planning untouched", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0", got.BackgroundPID)
	}
}

// TestReap_PutErrorWarns exercises the put-error branch: a store
// rooted at a path whose parent is unwritable rejects PutTask, the
// warning surfaces on stderr, and the reaper still returns the
// in-memory transition for the printer.
func TestReap_PutErrorWarns(t *testing.T) {
	id := "put-error"
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parent, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := tasks.Open(parent)
	var stderr bytes.Buffer
	in := []tasks.Task{{
		ID:            id,
		Status:        tasks.StatusPlanning,
		BackgroundPID: deadPID(t),
	}}
	out := reapBackgroundTasks(s, &stderr, parent, in)
	if out[0].Status != tasks.StatusHelp {
		t.Fatalf("in-memory row should still transition: %q", out[0].Status)
	}
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestReap_ListTasksWiresThroughCommand drives the cobra-wired path
// end-to-end: a `planning` row with a dead PID and on-disk
// artifacts must come out as `plan-done` after `j tasks` runs.
func TestReap_ListTasksWiresThroughCommand(t *testing.T) {
	s := openTestStore(t)
	id := "wired-task"
	seedTaskDir(t, id, "# wired heading\nbody", "1. step")
	begin := time.Now().UTC().Add(-time.Hour)
	row := tasks.Task{
		ID:            id,
		Status:        tasks.StatusPlanning,
		InvokedTool:   "cursor",
		InvokedModel:  "sonnet-4",
		BackgroundPID: deadPID(t),
		PlanBeginAt:   &begin,
	}
	if err := s.PutTask(row); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	out, _, err := runCommand(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, id) || !strings.Contains(out, "plan-done") {
		t.Fatalf("output should reflect reaped row: %q", out)
	}
	s2, err := tasks.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	persisted, err := s2.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if persisted.Status != tasks.StatusPlanDone {
		t.Fatalf("persisted Status = %q", persisted.Status)
	}
	if persisted.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0", persisted.BackgroundPID)
	}
}

// deadPID runs `true` to completion (cmd.Wait reaps the child) and
// returns its PID. By the time the function returns the child has
// exited so signal(0) yields ESRCH and run.IsAlive reports dead.
// PID reuse is theoretically possible but vanishingly unlikely
// within the few milliseconds between Wait and the reaper's IsAlive
// poll on a typical test host where PIDs increment sequentially.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run true: %v", err)
	}
	return cmd.Process.Pid
}
