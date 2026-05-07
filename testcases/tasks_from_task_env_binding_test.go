package testcases_test

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksFromTask_EnvBindings pins the acceptance bullet that each
// of the four leaves wires `--from-task` to a viper key + env var:
//
//   - TASKS_READ_REQUIREMENTS_FROM_TASK
//   - TASKS_READ_PLAN_FROM_TASK
//   - TASKS_LOGS_FROM_TASK
//   - TASKS_TASK_FROM_TASK
//
// Black-box: setting the env var to a ghost id makes the leaf
// short-circuit on the unknown-id branch (`J: no task`) instead of
// firing the picker (which would block on stdin in headless tests).
func TestTasksFromTask_EnvBindings(t *testing.T) {
	cases := []struct {
		name string
		env  string
		argv []string
	}{
		{"read requirements",
			"TASKS_READ_REQUIREMENTS_FROM_TASK",
			[]string{"read", "requirements"}},
		{"read plan",
			"TASKS_READ_PLAN_FROM_TASK",
			[]string{"read", "plan"}},
		{"logs", "TASKS_LOGS_FROM_TASK", []string{"logs"}},
		{"task", "TASKS_TASK_FROM_TASK", []string{"task"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Chdir(t.TempDir())
			testutil.Init(t)
			t.Setenv(tc.env, "ghost")

			stdout, _, err := testutil.RunCobra(
				tasks.New(), tc.argv...,
			)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !strings.Contains(stdout, "J: no task") {
				t.Fatalf(
					"stdout = %q, want substring `J: no task`",
					stdout,
				)
			}
		})
	}
}
