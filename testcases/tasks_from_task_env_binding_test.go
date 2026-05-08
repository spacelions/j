package testcases_test

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksFromTask_EnvBindings pins the acceptance bullet that each
// of the leaves wires `--from-task` to a viper key + env var:
//
//   - TASKS_SHOW_FROM_TASK
//   - TASKS_SHOW_REQUIREMENTS_FROM_TASK
//   - TASKS_SHOW_PLAN_FROM_TASK
//   - TASKS_LOGS_FROM_TASK
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
		{"show",
			"TASKS_SHOW_FROM_TASK",
			[]string{"show"}},
		{"show requirements",
			"TASKS_SHOW_REQUIREMENTS_FROM_TASK",
			[]string{"show", "requirements"}},
		{"show plan",
			"TASKS_SHOW_PLAN_FROM_TASK",
			[]string{"show", "plan"}},
		{"logs", "TASKS_LOGS_FROM_TASK", []string{"logs"}},
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
