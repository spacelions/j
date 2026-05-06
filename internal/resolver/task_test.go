package resolver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

type taskUI struct {
	pickID string
	ok     bool
	err    error

	confirm bool
	rows    []tasks.Task
}

func (u *taskUI) PickTask(_ context.Context, _ string, rows []tasks.Task) (string, bool, error) {
	u.rows = rows
	if u.err != nil {
		return "", false, u.err
	}
	return u.pickID, u.ok, nil
}

func (u *taskUI) ConfirmStatusOverride(context.Context, string, string, string) (bool, error) {
	return u.confirm, u.err
}

func setupResolverProject(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
}

func seedResolverTask(t *testing.T, row tasks.Task, plan, req string) {
	t.Helper()
	if row.ID == "" {
		t.Fatal("task id required")
	}
	dir, err := tasks.EnsureDir(row.ID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if plan != "" {
		if err := os.WriteFile(filepath.Join(dir, tasks.PlanFileName), []byte(plan), 0o644); err != nil {
			t.Fatalf("write plan: %v", err)
		}
	}
	if req != "" {
		if err := os.WriteFile(filepath.Join(dir, tasks.RequirementsFileName), []byte(req), 0o644); err != nil {
			t.Fatalf("write requirements: %v", err)
		}
	}
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(row); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
}

func TestTaskAllowlists(t *testing.T) {
	for _, status := range []tasks.TaskStatus{tasks.StatusPlanDone, tasks.StatusHelp} {
		if !ReplanAllowed(tasks.Task{Status: status}) {
			t.Fatalf("ReplanAllowed(%s) = false", status)
		}
	}
	if ReplanAllowed(tasks.Task{Status: tasks.StatusWorking}) {
		t.Fatal("working should not be allowed for replan")
	}
	for _, status := range []tasks.TaskStatus{tasks.StatusWorkDone, tasks.StatusFailed, tasks.StatusHelp} {
		if !VerifyAllowed(tasks.Task{Status: status}) {
			t.Fatalf("VerifyAllowed(%s) = false", status)
		}
	}
	if VerifyAllowed(tasks.Task{Status: tasks.StatusPlanDone}) {
		t.Fatal("plan-done should not be allowed for verify")
	}
}

func TestConfirmStatusOverride(t *testing.T) {
	ui := &taskUI{confirm: true}
	task := tasks.Task{ID: "t1", Status: tasks.StatusWorking}
	ok, err := ConfirmStatusOverride(context.Background(), ui, false, "work", task, ReplanAllowed)
	if err != nil || !ok {
		t.Fatalf("ConfirmStatusOverride = %v, %v", ok, err)
	}
	ui.confirm = false
	ok, err = ConfirmStatusOverride(context.Background(), ui, true, "work", task, ReplanAllowed)
	if err != nil || !ok {
		t.Fatalf("yes override = %v, %v", ok, err)
	}
	ok, err = ConfirmStatusOverride(context.Background(), ui, false, "work", tasks.Task{Status: tasks.StatusPlanDone}, ReplanAllowed)
	if err != nil || !ok {
		t.Fatalf("allowed status = %v, %v", ok, err)
	}
}

func TestResolveWorkPlan(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, tasks.Task{ID: "a", Status: tasks.StatusPlanDone}, "plan", "req")
	res, ok, err := ResolveWorkPlan(context.Background(), WorkPlanOptions{TaskID: "a", UI: &taskUI{}})
	if err != nil || !ok {
		t.Fatalf("ResolveWorkPlan by id = %+v, %v, %v", res, ok, err)
	}
	if res.Task.ID != "a" || res.Body != "plan" || res.Requirement != "req" {
		t.Fatalf("resolved work plan = %+v", res)
	}
}

func TestResolveWorkPlanAutoPicksSingleAllowedTask(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, tasks.Task{ID: "a", Status: tasks.StatusPlanning}, "a plan", "")
	seedResolverTask(t, tasks.Task{ID: "b", Status: tasks.StatusPlanDone}, "b plan", "")
	ui := &taskUI{err: errors.New("picker should not run")}
	res, ok, err := ResolveWorkPlan(context.Background(), WorkPlanOptions{UI: ui})
	if err != nil || !ok || res.Task.ID != "b" {
		t.Fatalf("auto resolve = %+v, %v, %v", res, ok, err)
	}
}

func TestResolveWorkPlanPickerPaths(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, tasks.Task{ID: "a", Status: tasks.StatusPlanning}, "a plan", "")
	seedResolverTask(t, tasks.Task{ID: "b", Status: tasks.StatusWorking}, "b plan", "")
	ui := &taskUI{pickID: "b", ok: true}
	res, ok, err := ResolveWorkPlan(context.Background(), WorkPlanOptions{UI: ui})
	if err != nil || !ok || res.Task.ID != "b" {
		t.Fatalf("picker resolve = %+v, %v, %v", res, ok, err)
	}
	ui = &taskUI{ok: false}
	_, ok, err = ResolveWorkPlan(context.Background(), WorkPlanOptions{UI: ui})
	if err != nil || ok {
		t.Fatalf("cancel = %v, %v", ok, err)
	}
	ui = &taskUI{err: errors.New("pick failed")}
	_, _, err = ResolveWorkPlan(context.Background(), WorkPlanOptions{UI: ui})
	if err == nil || !strings.Contains(err.Error(), "pick failed") {
		t.Fatalf("picker err = %v", err)
	}
}

func TestResolveWorkPlanEmptyAndMissing(t *testing.T) {
	setupResolverProject(t)
	_, _, err := ResolveWorkPlan(context.Background(), WorkPlanOptions{UI: &taskUI{}})
	if err == nil || !strings.Contains(err.Error(), "no tasks to work") {
		t.Fatalf("empty err = %v", err)
	}
	_, _, err = ResolveWorkPlan(context.Background(), WorkPlanOptions{TaskID: "missing", UI: &taskUI{}})
	if err == nil || !strings.Contains(err.Error(), `task "missing" not found`) {
		t.Fatalf("missing err = %v", err)
	}
	seedResolverTask(t, tasks.Task{ID: "noplan", Status: tasks.StatusPlanDone}, "", "")
	_, _, err = ResolveWorkPlan(context.Background(), WorkPlanOptions{TaskID: "noplan", UI: &taskUI{}})
	if err == nil || !strings.Contains(err.Error(), "work: read plan") {
		t.Fatalf("missing plan err = %v", err)
	}
}

func TestResolveVerifyTask(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, tasks.Task{ID: "v1", Status: tasks.StatusWorkDone}, "plan", "")
	res, ok, err := ResolveVerifyTask(context.Background(), VerifyTaskOptions{TaskID: "v1", UI: &taskUI{}})
	if err != nil || !ok {
		t.Fatalf("ResolveVerifyTask by id = %+v, %v, %v", res, ok, err)
	}
	if res.Task.ID != "v1" || filepath.Base(res.FindingsPath) != tasks.VerifierFindingsFileName {
		t.Fatalf("resolved verify task = %+v", res)
	}
}

func TestResolveVerifyTaskAutoPicksSingleAllowedTask(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, tasks.Task{ID: "a", Status: tasks.StatusPlanDone}, "a plan", "")
	seedResolverTask(t, tasks.Task{ID: "b", Status: tasks.StatusWorkDone}, "b plan", "")
	ui := &taskUI{err: errors.New("picker should not run")}
	res, ok, err := ResolveVerifyTask(context.Background(), VerifyTaskOptions{UI: ui})
	if err != nil || !ok || res.Task.ID != "b" {
		t.Fatalf("auto verify = %+v, %v, %v", res, ok, err)
	}
}

func TestResolveVerifyTaskPickerPaths(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, tasks.Task{ID: "a", Status: tasks.StatusPlanning}, "a plan", "")
	seedResolverTask(t, tasks.Task{ID: "b", Status: tasks.StatusWorking}, "b plan", "")
	ui := &taskUI{pickID: "b", ok: true}
	res, ok, err := ResolveVerifyTask(context.Background(), VerifyTaskOptions{UI: ui})
	if err != nil || !ok || res.Task.ID != "b" {
		t.Fatalf("picker resolve = %+v, %v, %v", res, ok, err)
	}
	ui = &taskUI{ok: false}
	_, ok, err = ResolveVerifyTask(context.Background(), VerifyTaskOptions{UI: ui})
	if err != nil || ok {
		t.Fatalf("cancel = %v, %v", ok, err)
	}
	ui = &taskUI{err: errors.New("pick failed")}
	_, _, err = ResolveVerifyTask(context.Background(), VerifyTaskOptions{UI: ui})
	if err == nil || !strings.Contains(err.Error(), "pick failed") {
		t.Fatalf("picker err = %v", err)
	}
}

func TestResolveVerifyTaskEmptyAndMissing(t *testing.T) {
	setupResolverProject(t)
	_, _, err := ResolveVerifyTask(context.Background(), VerifyTaskOptions{UI: &taskUI{}})
	if err == nil || !strings.Contains(err.Error(), "no tasks to verify") {
		t.Fatalf("empty err = %v", err)
	}
	_, _, err = ResolveVerifyTask(context.Background(), VerifyTaskOptions{TaskID: "missing", UI: &taskUI{}})
	if err == nil || !strings.Contains(err.Error(), `task "missing" not found`) {
		t.Fatalf("missing err = %v", err)
	}
	seedResolverTask(t, tasks.Task{ID: "noplan", Status: tasks.StatusWorkDone}, "", "")
	_, _, err = ResolveVerifyTask(context.Background(), VerifyTaskOptions{TaskID: "noplan", UI: &taskUI{}})
	if err == nil || !strings.Contains(err.Error(), "verify: read plan") {
		t.Fatalf("missing plan err = %v", err)
	}
}

func TestTaskStoreHelpers(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, tasks.Task{ID: "a", Status: tasks.StatusPlanDone}, "plan", "")
	row, err := TaskByID("a")
	if err != nil || row.ID != "a" {
		t.Fatalf("TaskByID = %+v, %v", row, err)
	}
	rows, err := ListAllTasks()
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListAllTasks = %+v, %v", rows, err)
	}
	if id, ok := autoPickAllowed(rows, ReplanAllowed); !ok || id != "a" {
		t.Fatalf("autoPickAllowed = %q, %v", id, ok)
	}
}
