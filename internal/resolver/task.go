package resolver

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/store"
)

type WorkPlan struct {
	Task        store.Task
	PlanPath    string
	Body        string
	Requirement string
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

type VerifyTask struct {
	Task             store.Task
	TaskDir          string
	RequirementsPath string
	PlanPath         string
	VerifierPlanPath string
	FindingsPath     string
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
