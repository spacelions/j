package tasks

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// openTestStore returns a tasks-mode *Store rooted in t.TempDir().
// The store is closed by t.Cleanup; tests that need the underlying
// path call DefaultTasksDir after.
func openTestStore(t *testing.T) *tasks.Store {
	t.Helper()
	t.Chdir(t.TempDir())
	testutil.Init(t)
	storeSeedPlanApprovalDisabled(t)
	s := tasks.OpenDefault()
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

// seedStaleLockFile creates the per-task `.lock` file with no kernel
// holder. TryAcquireForReap can flock it without contention, modeling
// the post-crash scenario where the previous holder exited (kernel
// released the flock automatically) but the file lingers on disk.
func seedStaleLockFile(t *testing.T, id string) {
	t.Helper()
	dir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	path := filepath.Join(dir, tasks.LockFileName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}
}

// seedHeldLock acquires the per-task `.lock` for the duration of the
// test and registers a Cleanup that releases it. The reaper's
// TryAcquireForReap will see the lock as held and skip the row,
// modeling a still-alive holder.
func seedHeldLock(t *testing.T, id string) {
	t.Helper()
	lock, err := tasks.AcquireLock(context.Background(), id)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })
}

// TestReap_LockHeldLeftAlone exercises the alive-holder branch: the
// per-task lock is held by the test process, so reapBackgroundTasks
// must NOT mutate the row.
func TestReap_LockHeldLeftAlone(t *testing.T) {
	s := openTestStore(t)
	tasksDir := tasks.DefaultDir()
	id := "live-task"
	seedHeldLock(t, id)
	begin := time.Now().UTC().Add(-time.Minute)
	in := []tasks.Task{{
		ID:           id,
		Status:       tasks.StatusPlanning,
		PlanBeginAt:  begin,
		Summary:      "alive",
		AgentLogPath: filepath.Join(tasksDir, id, "agent.log"),
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	if len(out) != 1 {
		t.Fatalf("len(out) = %d", len(out))
	}
	got := out[0]
	if got.Status != tasks.StatusPlanning {
		t.Fatalf("Status = %q, want planning", got.Status)
	}
	if !got.PlanEndAt.IsZero() {
		t.Fatalf("PlanEndAt should remain nil on live row: %v", got.PlanEndAt)
	}
}

// TestReap_DeadPlanning_WithArtifacts exercises the plan-done branch:
// the lock file is stale (no holder) and both requirements.md +
// plan.md exist on disk. The row flips to plan-done, summary refreshes
// from requirements, and PlanEndAt is stamped.
func TestReap_DeadPlanning_WithArtifacts(t *testing.T) {
	s := openTestStore(t)
	putProjectPlanRequiresApproval(t, "false")
	tasksDir := tasks.DefaultDir()
	id := "done-with-artifacts"
	seedTaskDir(t, id, "# refined heading\nbody", "1. step\n2. step")
	seedStaleLockFile(t, id)
	begin := time.Now().UTC().Add(-time.Minute)
	in := []tasks.Task{{
		ID:          id,
		Status:      tasks.StatusPlanning,
		PlanBeginAt: begin,
		Summary:     "stale",
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.PlanEndAt.IsZero() {
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

func TestReap_DeadPlanning_WithBlankSummaryKeepsExisting(t *testing.T) {
	s := openTestStore(t)
	putProjectPlanRequiresApproval(t, "false")
	tasksDir := tasks.DefaultDir()
	id := "done-blank-summary"
	seedTaskDir(t, id, "\n\t\n", "1. step")
	seedStaleLockFile(t, id)
	in := []tasks.Task{{
		ID:      id,
		Status:  tasks.StatusPlanning,
		Summary: "existing summary",
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.Summary != "existing summary" {
		t.Fatalf("Summary = %q, want existing summary", got.Summary)
	}
}

// TestReap_DeadPlanning_NoArtifacts pins the help-status branch: the
// lock is stale but neither requirements nor plan made it to disk
// (e.g. the spawned child crashed early), so the row flips to help.
func TestReap_DeadPlanning_NoArtifacts(t *testing.T) {
	s := openTestStore(t)
	tasksDir := tasks.DefaultDir()
	id := "dead-without-artifacts"
	seedTaskDir(t, id, "", "")
	seedStaleLockFile(t, id)
	in := []tasks.Task{{
		ID:      id,
		Status:  tasks.StatusPlanning,
		Summary: "fallback",
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help", got.Status)
	}
	if got.PlanEndAt.IsZero() {
		t.Fatalf("PlanEndAt should be stamped on help transition")
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
	tasksDir := tasks.DefaultDir()
	id := "dead-missing-plan"
	seedTaskDir(t, id, "# heading\nbody", "")
	seedStaleLockFile(t, id)
	in := []tasks.Task{{
		ID:     id,
		Status: tasks.StatusPlanning,
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	if out[0].Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help", out[0].Status)
	}
}

// TestReap_DeadWorking pins the work-done branch: a stale lock on a
// working row flips to work-done and stamps WorkEndAt. There is no
// artifact gate.
func TestReap_DeadWorking(t *testing.T) {
	s := openTestStore(t)
	tasksDir := tasks.DefaultDir()
	id := "dead-working"
	seedTaskDir(t, id, "", "")
	seedStaleLockFile(t, id)
	begin := time.Now().UTC().Add(-time.Minute)
	in := []tasks.Task{{
		ID:          id,
		Status:      tasks.StatusWorking,
		WorkBeginAt: begin,
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
	if got.WorkEndAt.IsZero() {
		t.Fatalf("WorkEndAt should be stamped")
	}
}

// TestReap_NonActiveStateUntouched pins the in-flight allowlist:
// rows in non-active statuses are ignored even when a stale lock file
// is present. Only planning and working transition through the reaper.
func TestReap_NonActiveStateUntouched(t *testing.T) {
	s := openTestStore(t)
	tasksDir := tasks.DefaultDir()
	id := "stale-help"
	seedTaskDir(t, id, "", "")
	seedStaleLockFile(t, id)
	in := []tasks.Task{{
		ID:      id,
		Status:  tasks.StatusHelp,
		Summary: "old",
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help (untouched)", got.Status)
	}
}

// TestReap_NoLockFileUntouched covers the no-lock-file short-circuit:
// foreground (interactive) and never-spawned rows have no per-task
// `.lock` on disk, so the helper returns them verbatim without any
// transition.
func TestReap_NoLockFileUntouched(t *testing.T) {
	s := openTestStore(t)
	tasksDir := tasks.DefaultDir()
	id := "no-lock"
	seedTaskDir(t, id, "", "")
	in := []tasks.Task{{
		ID:     id,
		Status: tasks.StatusPlanning,
	}}
	out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
	got := out[0]
	if got.Status != tasks.StatusPlanning {
		t.Fatalf("Status = %q, want planning untouched", got.Status)
	}
	if !got.PlanEndAt.IsZero() {
		t.Fatalf("PlanEndAt should remain zero: %v", got.PlanEndAt)
	}
}

// TestReap_PutErrorWarns exercises the put-error branch: a store
// rooted at a path whose parent is unwritable rejects PutTask, the
// warning surfaces on stderr, and the reaper still returns the
// in-memory transition for the printer.
func TestReap_PutErrorWarns(t *testing.T) {
	id := "put-error"
	t.Chdir(t.TempDir())
	testutil.Init(t)
	seedTaskDir(t, id, "", "")
	seedStaleLockFile(t, id)
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parent, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := tasks.Open(parent)
	var stderr bytes.Buffer
	in := []tasks.Task{{
		ID:     id,
		Status: tasks.StatusPlanning,
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
// end-to-end: a `planning` row with a stale lock and on-disk
// artifacts must come out as `plan-done` after `j tasks` runs.
func TestReap_ListTasksWiresThroughCommand(t *testing.T) {
	s := openTestStore(t)
	putProjectPlanRequiresApproval(t, "false")
	id := "wired-task"
	seedTaskDir(t, id, "# wired heading\nbody", "1. step")
	seedStaleLockFile(t, id)
	begin := time.Now().UTC().Add(-time.Hour)
	row := tasks.Task{
		ID:          id,
		Status:      tasks.StatusPlanning,
		PlanTool:    "cursor",
		PlanModel:   "sonnet-4",
		PlanBeginAt: begin,
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
	s2 := tasks.OpenDefault()
	defer func() { _ = s2.Close() }()
	persisted, err := s2.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if persisted.Status != tasks.StatusPlanDone {
		t.Fatalf("persisted Status = %q", persisted.Status)
	}
}

// TestReap_Dead_WithClarification pins the clarification branch: a
// stale lock with clarification.md present must flip to
// needs-clarification.
func TestReap_Dead_WithClarification(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		status    tasks.TaskStatus
		req, plan string
		makeTask  func(id string) tasks.Task
	}{
		{
			name:   "planning",
			id:     "dead-planning-clarification",
			status: tasks.StatusPlanning,
			req:    "# req\nbody",
			plan:   "1. step",
			makeTask: func(id string) tasks.Task {
				return tasks.Task{
					ID:          id,
					Status:      tasks.StatusPlanning,
					PlanBeginAt: time.Now().UTC().Add(-time.Minute),
				}
			},
		},
		{
			name:   "working",
			id:     "dead-working-clarification",
			status: tasks.StatusWorking,
			makeTask: func(id string) tasks.Task {
				return tasks.Task{
					ID:          id,
					Status:      tasks.StatusWorking,
					WorkBeginAt: time.Now().UTC().Add(-time.Minute),
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t)
			tasksDir := tasks.DefaultDir()
			dir := seedTaskDir(t, tc.id, tc.req, tc.plan)
			seedStaleLockFile(t, tc.id)
			if err := os.WriteFile(
				filepath.Join(dir, tasks.ClarificationFileName),
				[]byte("needs input\n"), 0o644,
			); err != nil {
				t.Fatal(err)
			}
			in := []tasks.Task{tc.makeTask(tc.id)}
			out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
			got := out[0]
			if got.Status != tasks.StatusNeedsClarification {
				t.Fatalf("Status = %q, want needs-clarification", got.Status)
			}
			persisted, err := s.GetTask(tc.id)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			if persisted.Status != tasks.StatusNeedsClarification {
				t.Fatalf("persisted Status = %q, want needs-clarification",
					persisted.Status)
			}
		})
	}
}

// reaperMarkerCase drives one reaper transition end-to-end and pins
// (a) the in-memory status flip, (b) one marker line in the per-task
// agent.log via the registered markersHook, and (c) the persisted row.
type reaperMarkerCase struct {
	name         string
	status       tasks.TaskStatus
	wantStatus   tasks.TaskStatus
	wantMarker   string
	approval     bool
	requirements string
	plan         string
	clarif       string
}

// TestReap_TransitionsEmitMarkers covers every reaper-driven event
// flowing through ApplyAndPersist plus markersHook. The table mirrors
// the four FSM edges reaper paths can take so a regression in either
// the transition table or the hook surfaces here.
func TestReap_TransitionsEmitMarkers(t *testing.T) {
	cases := []reaperMarkerCase{
		{
			name:         "plan_done",
			status:       tasks.StatusPlanning,
			wantStatus:   tasks.StatusPlanDone,
			wantMarker:   "plan done",
			requirements: "# heading\nbody",
			plan:         "1. step",
		},
		{
			name:         "plan_await_approval",
			status:       tasks.StatusPlanning,
			wantStatus:   tasks.StatusPlanPendingApproval,
			wantMarker:   "plan await approval",
			approval:     true,
			requirements: "# heading\nbody",
			plan:         "1. step",
		},
		{
			name:       "plan_fail",
			status:     tasks.StatusPlanning,
			wantStatus: tasks.StatusHelp,
			wantMarker: "plan fail",
		},
		{
			name:       "plan_needs_clarification",
			status:     tasks.StatusPlanning,
			wantStatus: tasks.StatusNeedsClarification,
			wantMarker: "plan needs clarification",
			clarif:     "what next?\n",
		},
		{
			name:       "work_done",
			status:     tasks.StatusWorking,
			wantStatus: tasks.StatusWorkDone,
			wantMarker: "work done",
		},
		{
			name:       "work_needs_clarification",
			status:     tasks.StatusWorking,
			wantStatus: tasks.StatusNeedsClarification,
			wantMarker: "work needs clarification",
			clarif:     "what branch?\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openTestStore(t)
			if c.approval {
				putProjectPlanRequiresApproval(t, "true")
			} else {
				putProjectPlanRequiresApproval(t, "false")
			}
			tasksDir := tasks.DefaultDir()
			id := "reap-" + c.name
			dir := seedTaskDir(t, id, c.requirements, c.plan)
			seedStaleLockFile(t, id)
			if c.clarif != "" {
				if err := os.WriteFile(
					filepath.Join(dir, tasks.ClarificationFileName),
					[]byte(c.clarif), 0o644,
				); err != nil {
					t.Fatal(err)
				}
			}
			logPath := filepath.Join(dir, tasks.AgentLogFileName)
			t.Cleanup(tasks.ResetHooksForTest)
			lifecycle.Init()
			in := []tasks.Task{{
				ID:           id,
				Status:       c.status,
				AgentLogPath: logPath,
			}}
			out := reapBackgroundTasks(s, io.Discard, tasksDir, in)
			got := out[0]
			if got.Status != c.wantStatus {
				t.Fatalf("Status = %q, want %q",
					got.Status, c.wantStatus)
			}
			persisted, err := s.GetTask(id)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			if persisted.Status != c.wantStatus {
				t.Fatalf("persisted Status = %q, want %q",
					persisted.Status, c.wantStatus)
			}
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("read agent.log: %v", err)
			}
			body := string(data)
			if !strings.Contains(body, c.wantMarker) {
				t.Fatalf("agent.log missing %q: %q",
					c.wantMarker, body)
			}
			if lines := strings.Count(
				strings.TrimSpace(body), "\n",
			); lines != 0 {
				t.Fatalf("want exactly one marker line, got %d in %q",
					lines+1, body)
			}
		})
	}
}

func storeSeedPlanApprovalDisabled(t *testing.T) {
	t.Helper()
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.EnsureBucket(store.BucketProject); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketProject,
		store.KeyPlanRequiresApproval, "false"); err != nil {
		t.Fatal(err)
	}
}
