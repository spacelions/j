package resolver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/store/tasks"
)

type StatusOverrideUI interface {
	ConfirmStatusOverride(
		ctx context.Context, cmd, taskID, status string,
	) (bool, error)
}

func ConfirmStatusOverride(
	ctx context.Context, ui StatusOverrideUI, yes bool, cmd string,
	t tasks.Task, allowed func(tasks.Task) bool,
) (bool, error) {
	if allowed(t) || yes {
		return true, nil
	}
	return ui.ConfirmStatusOverride(ctx, cmd, t.ID, string(t.Status))
}

func ReplanAllowed(t tasks.Task) bool {
	switch t.Status {
	case tasks.StatusPlanDone, tasks.StatusHelp:
		return true
	default:
		return false
	}
}

func VerifyAllowed(t tasks.Task) bool {
	switch t.Status {
	case tasks.StatusWorkDone, tasks.StatusFailed, tasks.StatusHelp:
		return true
	default:
		return false
	}
}

type WorkPlanUI interface {
	PickTask(
		ctx context.Context, title string, tasks []tasks.Task,
	) (string, bool, error)
}

type WorkPlanOptions struct {
	TaskID string
	UI     WorkPlanUI
}

type WorkPlan struct {
	Task        tasks.Task
	TaskDir     string
	Paths       tasks.TaskPaths
	Body        string
	Requirement string
}

func ResolveWorkPlan(
	ctx context.Context, opts WorkPlanOptions,
) (WorkPlan, bool, error) {
	if opts.TaskID != "" {
		r, err := resolveWorkByTaskID(opts.TaskID)
		return r, err == nil, err
	}
	rows, err := listResolvableTasks()
	if err != nil {
		return WorkPlan{}, false, err
	}
	if len(rows) == 0 {
		return WorkPlan{}, false, errors.New(
			"no tasks to work; run `j plan` first")
	}
	if id, ok := autoPickAllowed(rows, ReplanAllowed); ok {
		r, err := resolveWorkByTaskID(id)
		return r, err == nil, err
	}
	chosen, ok, err := opts.UI.PickTask(
		ctx, "Select a task to work", rows)
	if err != nil || !ok {
		return WorkPlan{}, false, err
	}
	r, err := resolveWorkByTaskID(chosen)
	return r, err == nil, err
}

func resolveWorkByTaskID(id string) (WorkPlan, error) {
	row, err := TaskByID(id)
	if err != nil {
		return WorkPlan{}, err
	}
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return WorkPlan{}, err
	}
	taskDir := filepath.Join(tasksDir, id)
	paths := taskPaths(taskDir)
	body, err := os.ReadFile(paths.Plan)
	if err != nil {
		return WorkPlan{}, fmt.Errorf("work: read plan: %w", err)
	}
	var requirement string
	if data, readErr := os.ReadFile(paths.Requirements); readErr == nil {
		requirement = string(data)
	}
	return WorkPlan{
		Task:        row,
		TaskDir:     taskDir,
		Paths:       paths,
		Body:        string(body),
		Requirement: requirement,
	}, nil
}

type VerifyTaskUI interface {
	PickTask(
		ctx context.Context, title string, tasks []tasks.Task,
	) (string, bool, error)
}

type VerifyTaskOptions struct {
	TaskID string
	UI     VerifyTaskUI
}

type VerifyTask struct {
	Task    tasks.Task
	TaskDir string
	Paths   tasks.TaskPaths
}

type PlanTask struct {
	Task    tasks.Task
	TaskDir string
	Paths   tasks.TaskPaths
}

func ResolvePlanTask(taskID string) (PlanTask, error) {
	row, err := TaskByID(taskID)
	if err != nil {
		return PlanTask{}, err
	}
	taskDir, err := taskDirFor(taskID)
	if err != nil {
		return PlanTask{}, err
	}
	return PlanTask{
		Task:    row,
		TaskDir: taskDir,
		Paths:   taskPaths(taskDir),
	}, nil
}

func ResolveVerifyTask(
	ctx context.Context, opts VerifyTaskOptions,
) (VerifyTask, bool, error) {
	if opts.TaskID != "" {
		r, err := resolveVerifyByTaskID(opts.TaskID)
		return r, err == nil, err
	}
	rows, err := listResolvableTasks()
	if err != nil {
		return VerifyTask{}, false, err
	}
	if len(rows) == 0 {
		return VerifyTask{}, false, errors.New(
			"no tasks to verify; run `j plan` and `j work` first")
	}
	if id, ok := autoPickAllowed(rows, VerifyAllowed); ok {
		r, err := resolveVerifyByTaskID(id)
		return r, err == nil, err
	}
	chosen, ok, err := opts.UI.PickTask(
		ctx, "Select a task to verify", rows)
	if err != nil || !ok {
		return VerifyTask{}, false, err
	}
	r, err := resolveVerifyByTaskID(chosen)
	return r, err == nil, err
}

func resolveVerifyByTaskID(id string) (VerifyTask, error) {
	row, err := TaskByID(id)
	if err != nil {
		return VerifyTask{}, err
	}
	taskDir, err := taskDirFor(id)
	if err != nil {
		return VerifyTask{}, err
	}
	paths := taskPaths(taskDir)
	if _, err := os.Stat(paths.Plan); err != nil {
		return VerifyTask{}, fmt.Errorf("verify: read plan: %w", err)
	}
	return VerifyTask{
		Task:    row,
		TaskDir: taskDir,
		Paths:   paths,
	}, nil
}

func ListAllTasks() ([]tasks.Task, error) {
	return listResolvableTasks()
}

func TaskByID(id string) (tasks.Task, error) {
	s, err := openTaskStore()
	if err != nil {
		return tasks.Task{}, err
	}
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return tasks.Task{}, fmt.Errorf("task %q not found", id)
		}
		return tasks.Task{}, err
	}
	return row, nil
}

func listResolvableTasks() ([]tasks.Task, error) {
	s, err := openTaskStore()
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	tasks.SortTasks(all)
	return all, nil
}

func autoPickAllowed(
	rows []tasks.Task, allowed func(tasks.Task) bool,
) (string, bool) {
	var picked string
	count := 0
	for _, row := range rows {
		if allowed(row) {
			picked = row.ID
			count++
		}
	}
	return picked, count == 1
}

func openTaskStore() (*tasks.Store, error) {
	s, err := tasks.OpenDefault()
	if err != nil {
		return nil, fmt.Errorf("tasks: open store: %w", err)
	}
	return s, nil
}

func taskDirFor(id string) (string, error) {
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(tasksDir, id), nil
}

func taskPaths(taskDir string) tasks.TaskPaths {
	return tasks.TaskPaths{
		Requirements: filepath.Join(taskDir, tasks.RequirementsFileName),
		Plan:         filepath.Join(taskDir, tasks.PlanFileName),
		VerifierPlan: filepath.Join(taskDir, tasks.VerifierPlanFileName),
		Findings:     filepath.Join(taskDir, tasks.VerifierFindingsFileName),
		Clarification: filepath.Join(
			taskDir,
			tasks.ClarificationFileName,
		),
	}
}
