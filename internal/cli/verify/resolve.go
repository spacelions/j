package verify

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spacelions/j/internal/cli/picker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
)

func resolveTask(ctx context.Context, opts Options) (resolved, bool, error) {
	if opts.TaskID != "" {
		r, err := resolveByTaskID(opts.TaskID)
		return r, err == nil, err
	}
	tasks, err := listResolvableVerifyTasks()
	if err != nil {
		return resolved{}, false, err
	}
	if len(tasks) == 0 {
		return resolved{}, false, errors.New("J: no tasks to verify; run `j plan` and `j work` first")
	}
	if id, ok := autoPickAllowed(tasks, allowedForVerify); ok {
		r, err := resolveByTaskID(id)
		return r, err == nil, err
	}
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a task to verify", tasks)
	if err != nil {
		return resolved{}, false, err
	}
	if !ok {
		return resolved{}, false, nil
	}
	r, err := resolveByTaskID(chosen)
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

func resolveByTaskID(id string) (resolved, error) {
	s, err := openTasks()
	if err != nil {
		return resolved{}, err
	}
	defer func() { _ = s.Close() }()
	task, err := s.GetTask(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return resolved{}, fmt.Errorf("verify: task %q not found", id)
		}
		return resolved{}, err
	}
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return resolved{}, err
	}
	taskDir := filepath.Join(tasksDir, id)
	planPath := filepath.Join(taskDir, store.PlanFileName)
	if _, err := os.Stat(planPath); err != nil {
		return resolved{}, fmt.Errorf("verify: read plan: %w", err)
	}
	return resolved{
		Task:             task,
		TaskDir:          taskDir,
		RequirementsPath: filepath.Join(taskDir, store.RequirementsFileName),
		PlanPath:         planPath,
		VerifierPlanPath: filepath.Join(taskDir, store.VerifierPlanFileName),
		FindingsPath:     filepath.Join(taskDir, store.VerifierFindingsFileName),
	}, nil
}

func listResolvableVerifyTasks() ([]store.Task, error) {
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
		return nil, fmt.Errorf("verify: tasks db: %w", err)
	}
	s, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("verify: tasks db: %w", err)
	}
	return s, nil
}

func allowedForVerify(t store.Task) bool {
	switch t.Status {
	case store.StatusWorkDone, store.StatusVerifyDone, store.StatusHelp:
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

func ReadVerdictForTask(taskID string) string {
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return "FAIL"
	}
	return ParseVerdict(filepath.Join(tasksDir, taskID, store.VerifierFindingsFileName))
}

var verdictRegexp = regexp.MustCompile(`(?i)^\s*VERDICT:\s*(PASS|FAIL)\s*$`)

func ParseVerdict(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "FAIL"
	}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := verdictRegexp.FindStringSubmatch(line)
		if m == nil {
			return "FAIL"
		}
		return strings.ToUpper(m[1])
	}
	return "FAIL"
}

func selectVerifier(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	return resolver.Agent(ctx, resolver.AgentOptions{
		Bucket:        store.BucketVerifier,
		Agents:        opts.Agents,
		ExplicitTool:  opts.Tool,
		ExplicitModel: opts.Model,
		UI:            opts.UI,
		Store:         opts.Store,
		Stderr:        opts.Stderr,
		Interactive:   opts.Interactive,
	})
}

func lookupResumeAgent(agents []codingagents.Agent, tool string) (codingagents.Agent, bool) {
	for _, a := range agents {
		if a.Name() == tool {
			return a, true
		}
	}
	return nil, false
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
	if o.MaxIterations <= 0 {
		o.MaxIterations = defaultMaxIterations
	}
	return o
}
