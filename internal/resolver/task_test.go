package resolver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

type taskUI struct {
	pickID string
	ok     bool
	err    error

	confirm bool
	tasks   []store.Task
}

func (u *taskUI) PickTask(_ context.Context, _ string, tasks []store.Task) (string, bool, error) {
	u.tasks = tasks
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

func seedResolverTask(t *testing.T, task store.Task, plan, req string) {
	t.Helper()
	if task.ID == "" {
		t.Fatal("task id required")
	}
	dir, err := store.EnsureTaskDir(task.ID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if plan != "" {
		if err := os.WriteFile(filepath.Join(dir, store.PlanFileName), []byte(plan), 0o644); err != nil {
			t.Fatalf("write plan: %v", err)
		}
	}
	if req != "" {
		if err := os.WriteFile(filepath.Join(dir, store.RequirementsFileName), []byte(req), 0o644); err != nil {
			t.Fatalf("write requirements: %v", err)
		}
	}
	path, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	s := store.OpenTasks(path)
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
}

func TestTaskAllowlists(t *testing.T) {
	for _, status := range []store.TaskStatus{store.StatusPlanDone, store.StatusHelp} {
		if !ReplanAllowed(store.Task{Status: status}) {
			t.Fatalf("ReplanAllowed(%s) = false", status)
		}
	}
	if ReplanAllowed(store.Task{Status: store.StatusWorking}) {
		t.Fatal("working should not be allowed for replan")
	}
	for _, status := range []store.TaskStatus{store.StatusWorkDone, store.StatusVerifyDone, store.StatusHelp} {
		if !VerifyAllowed(store.Task{Status: status}) {
			t.Fatalf("VerifyAllowed(%s) = false", status)
		}
	}
	if VerifyAllowed(store.Task{Status: store.StatusPlanDone}) {
		t.Fatal("plan-done should not be allowed for verify")
	}
}

func TestConfirmStatusOverride(t *testing.T) {
	ui := &taskUI{confirm: true}
	task := store.Task{ID: "t1", Status: store.StatusWorking}
	ok, err := ConfirmStatusOverride(context.Background(), ui, false, "work", task, ReplanAllowed)
	if err != nil || !ok {
		t.Fatalf("ConfirmStatusOverride = %v, %v", ok, err)
	}
	ui.confirm = false
	ok, err = ConfirmStatusOverride(context.Background(), ui, true, "work", task, ReplanAllowed)
	if err != nil || !ok {
		t.Fatalf("yes override = %v, %v", ok, err)
	}
	ok, err = ConfirmStatusOverride(context.Background(), ui, false, "work", store.Task{Status: store.StatusPlanDone}, ReplanAllowed)
	if err != nil || !ok {
		t.Fatalf("allowed status = %v, %v", ok, err)
	}
}

func TestResolveWorkPlan(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, store.Task{ID: "a", Status: store.StatusPlanDone}, "plan", "req")
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
	seedResolverTask(t, store.Task{ID: "a", Status: store.StatusPlanning}, "a plan", "")
	seedResolverTask(t, store.Task{ID: "b", Status: store.StatusPlanDone}, "b plan", "")
	ui := &taskUI{err: errors.New("picker should not run")}
	res, ok, err := ResolveWorkPlan(context.Background(), WorkPlanOptions{UI: ui})
	if err != nil || !ok || res.Task.ID != "b" {
		t.Fatalf("auto resolve = %+v, %v, %v", res, ok, err)
	}
}

func TestResolveWorkPlanPickerPaths(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, store.Task{ID: "a", Status: store.StatusPlanning}, "a plan", "")
	seedResolverTask(t, store.Task{ID: "b", Status: store.StatusWorking}, "b plan", "")
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
	seedResolverTask(t, store.Task{ID: "noplan", Status: store.StatusPlanDone}, "", "")
	_, _, err = ResolveWorkPlan(context.Background(), WorkPlanOptions{TaskID: "noplan", UI: &taskUI{}})
	if err == nil || !strings.Contains(err.Error(), "work: read plan") {
		t.Fatalf("missing plan err = %v", err)
	}
}

func TestResolveVerifyTask(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, store.Task{ID: "v1", Status: store.StatusWorkDone}, "plan", "")
	res, ok, err := ResolveVerifyTask(context.Background(), VerifyTaskOptions{TaskID: "v1", UI: &taskUI{}})
	if err != nil || !ok {
		t.Fatalf("ResolveVerifyTask by id = %+v, %v, %v", res, ok, err)
	}
	if res.Task.ID != "v1" || filepath.Base(res.FindingsPath) != store.VerifierFindingsFileName {
		t.Fatalf("resolved verify task = %+v", res)
	}
}

func TestResolveVerifyTaskAutoPicksSingleAllowedTask(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, store.Task{ID: "a", Status: store.StatusPlanDone}, "a plan", "")
	seedResolverTask(t, store.Task{ID: "b", Status: store.StatusWorkDone}, "b plan", "")
	ui := &taskUI{err: errors.New("picker should not run")}
	res, ok, err := ResolveVerifyTask(context.Background(), VerifyTaskOptions{UI: ui})
	if err != nil || !ok || res.Task.ID != "b" {
		t.Fatalf("auto verify = %+v, %v, %v", res, ok, err)
	}
}

func TestResolveVerifyTaskPickerPaths(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, store.Task{ID: "a", Status: store.StatusPlanning}, "a plan", "")
	seedResolverTask(t, store.Task{ID: "b", Status: store.StatusWorking}, "b plan", "")
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
	seedResolverTask(t, store.Task{ID: "noplan", Status: store.StatusWorkDone}, "", "")
	_, _, err = ResolveVerifyTask(context.Background(), VerifyTaskOptions{TaskID: "noplan", UI: &taskUI{}})
	if err == nil || !strings.Contains(err.Error(), "verify: read plan") {
		t.Fatalf("missing plan err = %v", err)
	}
}

func TestTaskStoreHelpers(t *testing.T) {
	setupResolverProject(t)
	seedResolverTask(t, store.Task{ID: "a", Status: store.StatusPlanDone}, "plan", "")
	task, err := TaskByID("test", "a")
	if err != nil || task.ID != "a" {
		t.Fatalf("TaskByID = %+v, %v", task, err)
	}
	tasks, err := ListTasks("test")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("ListTasks = %+v, %v", tasks, err)
	}
	tasks, err = ListAllTasks()
	if err != nil || len(tasks) != 1 {
		t.Fatalf("ListAllTasks = %+v, %v", tasks, err)
	}
	if id, ok := autoPickAllowed(tasks, ReplanAllowed); !ok || id != "a" {
		t.Fatalf("autoPickAllowed = %q, %v", id, ok)
	}
}

