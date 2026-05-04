package resolver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/store"
)

type StatusOverrideUI interface {
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}

func ConfirmStatusOverride(ctx context.Context, ui StatusOverrideUI, yes bool, cmd string, task store.Task, allowed func(store.Task) bool) (bool, error) {
	if allowed(task) || yes {
		return true, nil
	}
	return ui.ConfirmStatusOverride(ctx, cmd, task.ID, string(task.Status))
}

func ReplanAllowed(task store.Task) bool {
	switch task.Status {
	case store.StatusPlanDone, store.StatusHelp:
		return true
	}
	return false
}

func VerifyAllowed(task store.Task) bool {
	switch task.Status {
	case store.StatusWorkDone, store.StatusVerifyDone, store.StatusHelp:
		return true
	}
	return false
}

type WorkPlanUI interface {
	PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error)
}

type WorkPlanOptions struct {
	TaskID string
	UI     WorkPlanUI
}

type WorkPlan struct {
	Task        store.Task
	PlanPath    string
	Body        string
	Requirement string
}

func ResolveWorkPlan(ctx context.Context, opts WorkPlanOptions) (WorkPlan, bool, error) {
	switch {
	case opts.TaskID != "":
		r, err := resolveWorkByTaskID(opts.TaskID)
		return r, err == nil, err
	}
	tasks, err := listResolvableTasks("work")
	if err != nil {
		return WorkPlan{}, false, err
	}
	if len(tasks) == 0 {
		return WorkPlan{}, false, errors.New("J: no tasks to work; run `j plan` first")
	}
	if id, ok := autoPickAllowed(tasks, ReplanAllowed); ok {
		r, err := resolveWorkByTaskID(id)
		return r, err == nil, err
	}
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a task to work", tasks)
	if err != nil || !ok {
		return WorkPlan{}, false, err
	}
	r, err := resolveWorkByTaskID(chosen)
	return r, err == nil, err
}

func resolveWorkByTaskID(id string) (WorkPlan, error) {
	task, err := TaskByID("work", id)
	if err != nil {
		return WorkPlan{}, err
	}
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return WorkPlan{}, err
	}
	taskDir := filepath.Join(tasksDir, id)
	planPath := filepath.Join(taskDir, store.PlanFileName)
	body, err := os.ReadFile(planPath)
	if err != nil {
		return WorkPlan{}, fmt.Errorf("work: read plan: %w", err)
	}
	var requirement string
	if data, readErr := os.ReadFile(filepath.Join(taskDir, store.RequirementsFileName)); readErr == nil {
		requirement = string(data)
	}
	return WorkPlan{Task: task, PlanPath: planPath, Body: string(body), Requirement: requirement}, nil
}

type VerifyTaskUI interface {
	PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error)
}

type VerifyTaskOptions struct {
	TaskID string
	UI     VerifyTaskUI
}

type VerifyTask struct {
	Task             store.Task
	TaskDir          string
	RequirementsPath string
	PlanPath         string
	VerifierPlanPath string
	FindingsPath     string
}

func ResolveVerifyTask(ctx context.Context, opts VerifyTaskOptions) (VerifyTask, bool, error) {
	if opts.TaskID != "" {
		r, err := resolveVerifyByTaskID(opts.TaskID)
		return r, err == nil, err
	}
	tasks, err := listResolvableTasks("verify")
	if err != nil {
		return VerifyTask{}, false, err
	}
	if len(tasks) == 0 {
		return VerifyTask{}, false, errors.New("J: no tasks to verify; run `j plan` and `j work` first")
	}
	if id, ok := autoPickAllowed(tasks, VerifyAllowed); ok {
		r, err := resolveVerifyByTaskID(id)
		return r, err == nil, err
	}
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a task to verify", tasks)
	if err != nil || !ok {
		return VerifyTask{}, false, err
	}
	r, err := resolveVerifyByTaskID(chosen)
	return r, err == nil, err
}

func resolveVerifyByTaskID(id string) (VerifyTask, error) {
	task, err := TaskByID("verify", id)
	if err != nil {
		return VerifyTask{}, err
	}
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return VerifyTask{}, err
	}
	taskDir := filepath.Join(tasksDir, id)
	planPath := filepath.Join(taskDir, store.PlanFileName)
	if _, err := os.Stat(planPath); err != nil {
		return VerifyTask{}, fmt.Errorf("verify: read plan: %w", err)
	}
	return VerifyTask{
		Task:             task,
		TaskDir:          taskDir,
		RequirementsPath: filepath.Join(taskDir, store.RequirementsFileName),
		PlanPath:         planPath,
		VerifierPlanPath: filepath.Join(taskDir, store.VerifierPlanFileName),
		FindingsPath:     filepath.Join(taskDir, store.VerifierFindingsFileName),
	}, nil
}

func ListAllTasks() ([]store.Task, error) {
	return listResolvableTasks("tasks")
}

func ListTasks(prefix string) ([]store.Task, error) {
	return listResolvableTasks(prefix)
}

func TaskByID(prefix, id string) (store.Task, error) {
	s, err := openTaskStore(prefix)
	if err != nil {
		return store.Task{}, err
	}
	defer func() { _ = s.Close() }()
	task, err := s.GetTask(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return store.Task{}, fmt.Errorf("%s: task %q not found", prefix, id)
		}
		return store.Task{}, err
	}
	return task, nil
}

func listResolvableTasks(prefix string) ([]store.Task, error) {
	s, err := openTaskStore(prefix)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	store.SortTasks(all)
	return all, nil
}

func autoPickAllowed(tasks []store.Task, allowed func(store.Task) bool) (string, bool) {
	var picked string
	count := 0
	for _, task := range tasks {
		if allowed(task) {
			picked = task.ID
			count++
		}
	}
	return picked, count == 1
}

func openTaskStore(prefix string) (*store.Store, error) {
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		return nil, fmt.Errorf("%s: tasks db: %w", prefix, err)
	}
	s, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s: tasks db: %w", prefix, err)
	}
	return s, nil
}
