package work

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/cli/picker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/mdfile"
)

func resolvePlan(ctx context.Context, opts Options) (resolved, bool, error) {
	switch {
	case opts.TaskID != "":
		r, err := resolveByTaskID(opts, opts.TaskID)
		return r, err == nil, err
	case opts.FromFile != "":
		r, err := resolveFromFile(opts, opts.FromFile)
		return r, err == nil, err
	}
	tasks, err := listResolvableWorkTasks()
	if err != nil {
		return resolved{}, false, err
	}
	if len(tasks) == 0 {
		raw, err := opts.UI.AskFromFile(ctx)
		if err != nil {
			return resolved{}, false, err
		}
		r, err := resolveFromFile(opts, raw)
		return r, err == nil, err
	}
	if id, ok := autoPickAllowed(tasks, allowedForWork); ok {
		r, err := resolveByTaskID(opts, id)
		return r, err == nil, err
	}
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a task to work", tasks)
	if err != nil {
		return resolved{}, false, err
	}
	if !ok {
		return resolved{}, false, nil
	}
	r, err := resolveByTaskID(opts, chosen)
	return r, err == nil, err
}

func autoPickAllowed(tasks []store.Task, allowed func(store.Task) bool) (string, bool) {
	var picked string
	count := 0
	for _, t := range tasks {
		if allowed(t) {
			picked = t.ID
			count++
		}
	}
	if count == 1 {
		return picked, true
	}
	return "", false
}

func resolveByTaskID(opts Options, id string) (resolved, error) {
	s, err := openTasks()
	if err != nil {
		return resolved{}, err
	}
	defer func() { _ = s.Close() }()
	task, err := s.GetTask(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return resolved{}, fmt.Errorf("work: task %q not found", id)
		}
		return resolved{}, err
	}
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return resolved{}, err
	}
	taskDir := filepath.Join(tasksDir, id)
	planPath := filepath.Join(taskDir, store.PlanFileName)
	body, err := os.ReadFile(planPath)
	if err != nil {
		return resolved{}, fmt.Errorf("work: read plan: %w", err)
	}
	var requirement string
	if data, readErr := os.ReadFile(filepath.Join(taskDir, store.RequirementsFileName)); readErr == nil {
		requirement = string(data)
	}
	return resolved{
		Existing:    &task,
		PlanPath:    planPath,
		Body:        string(body),
		Requirement: requirement,
	}, nil
}

func resolveFromFile(opts Options, raw string) (resolved, error) {
	src, err := mdfile.Resolve(raw)
	if err != nil {
		return resolved{}, err
	}
	body, err := os.ReadFile(src)
	if err != nil {
		return resolved{}, fmt.Errorf("work: read plan: %w", err)
	}
	requirement := store.ReadRequirementSidecar(src)

	id := store.NewTaskID()
	dir, err := store.EnsureTaskDir(id)
	if err != nil {
		return resolved{}, fmt.Errorf("work: ensure task dir: %w", err)
	}
	planPath := filepath.Join(dir, store.PlanFileName)
	if err := os.WriteFile(planPath, body, 0o644); err != nil {
		return resolved{}, fmt.Errorf("work: write plan: %w", err)
	}
	if requirement != "" {
		reqPath := filepath.Join(dir, store.RequirementsFileName)
		if err := os.WriteFile(reqPath, []byte(requirement), 0o644); err != nil {
			banner.DangerousFprintf(opts.Stderr, "J: warning: write requirements: %v\n", err)
		}
	}
	return resolved{
		PlanPath:    planPath,
		Body:        string(body),
		Requirement: requirement,
		NewTaskID:   id,
	}, nil
}

func listResolvableWorkTasks() ([]store.Task, error) {
	s, err := openTasks()
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

func openTasks() (*store.Store, error) {
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		return nil, fmt.Errorf("work: tasks db: %w", err)
	}
	s, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("work: tasks db: %w", err)
	}
	return s, nil
}

func allowedForWork(t store.Task) bool {
	switch t.Status {
	case store.StatusPlanDone, store.StatusHelp:
		return true
	}
	return false
}

func confirmStatusOverride(ctx context.Context, opts Options, cmd string, t store.Task, allowed func(store.Task) bool) (bool, error) {
	if allowed(t) {
		return true, nil
	}
	if opts.Yes {
		return true, nil
	}
	return opts.UI.ConfirmStatusOverride(ctx, cmd, t.ID, string(t.Status))
}

func selectWorker(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	return resolver.Agent(ctx, resolver.AgentOptions{
		Bucket:        store.BucketWorker,
		Agents:        opts.Agents,
		ExplicitTool:  opts.Tool,
		ExplicitModel: opts.Model,
		UI:            opts.UI,
		Store:         opts.Store,
		Stderr:        opts.Stderr,
		Interactive:   opts.Interactive,
	})
}

func (o Options) withDefaults() Options {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.UI == nil {
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	return o
}
