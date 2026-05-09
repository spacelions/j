package tasks

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/linear"
)

// StartUI is the slice of picker methods RunStart drives when
// `--from-file` / `--from-task` are empty: the source picker
// (markdown | linear | task), the markdown / re-plan / Linear
// sub-pickers, and the status-override confirmation. *picker.Picker
// satisfies this surface; tests inject a scripted fake.
type StartUI interface {
	SelectSource(ctx context.Context,
		allowed []picker.Source) (picker.Source, error)
	PickMarkdownInCwd(ctx context.Context) (string, error)
	PickTask(ctx context.Context,
		title string, tasks []tasks.Task) (string, bool, error)
	PromptLinearAPIKey(ctx context.Context,
		openURL string) (string, bool, error)
	PickLinearProject(ctx context.Context,
		projects []linear.Project) (linear.Project, bool, error)
	PickLinearIssue(ctx context.Context,
		issues []linear.Issue) (linear.Issue, bool, error)
	ConfirmStatusOverride(ctx context.Context,
		cmd, taskID, status string) (bool, error)
}

func persistStartRow(stderr io.Writer, target startTarget,
	agentLogPath string, pid int,
) {
	if target.IsNew {
		t := tasks.Task{
			ID:            target.TaskID,
			Summary:       tasks.Summary(target.Body, target.Source),
			PlanBeginAt:   time.Now().UTC(),
			AgentLogPath:  agentLogPath,
			BackgroundPID: pid,
			LinearIssue:   target.LinearIssue,
		}
		if _, err := tasks.ApplyAndPersistWarn(
			stderr, &t, tasks.EventPlanBegin); err != nil {
			panic("plan begin from zero value: " + err.Error())
		}
		return
	}
	stampSpawnOnRow(stderr, target.TaskID, agentLogPath, pid)
}

func resolveJBinary(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve j binary: %w", err)
	}
	return exe, nil
}

func resolvePlanRequiresApproval(override *bool) (bool, error) {
	if override != nil {
		return *override, nil
	}
	return store.LoadPlanRequiresApproval()
}

func (o StartOptions) withDefaults() StartOptions {
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

func startPlanRequiresApprovalOverride(cmd *cobra.Command) (*bool, error) {
	approvalSet := cmd.Flags().Changed("plan-requires-approval") ||
		envSet("TASKS_START_PLAN_REQUIRES_APPROVAL")
	if approvalSet {
		v := viper.GetBool("tasks.start.plan_requires_approval")
		return &v, nil
	}
	return nil, nil
}

func envSet(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}

// bindStartFlags registers every `j tasks start` flag and binds it to
// its viper key + env var. Extracted from newStartCmd to keep that
// function under the 80-line method cap.
func bindStartFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("from-file", "f", "",
		"Path to a markdown file describing the task")
	cmd.Flags().String("from-linear", "",
		"Linear issue identifier (e.g. ENG-123); "+
			"requires linear.api_key in settings")
	cmd.Flags().String(flagKeyFromTask, "",
		"Existing task id to re-plan in place")
	cmd.Flags().String(flagKeyTool, "",
		"Planner tool override (cursor|claude); does not update bucket")
	cmd.Flags().String(flagKeyModel, "",
		"Planner model override; does not update the bucket")
	cmd.Flags().Bool(flagKeyInteractive, false,
		"Run planner in interactive (TUI) mode")
	cmd.Flags().BoolP("yes", "y", false,
		"Skip status-mismatch confirmation when re-planning")
	cmd.Flags().Bool("plan-requires-approval", false,
		"Override project.plan_requires_approval for this run "+
			"(use =false to skip once)")
	bindings := []struct{ key, flag, env string }{
		{"tasks.start.from_file", "from-file", "TASKS_START_FROM_FILE"},
		{
			"tasks.start.from_linear", "from-linear",
			"TASKS_START_FROM_LINEAR",
		},
		{"tasks.start.from_task", flagKeyFromTask, "TASKS_START_FROM_TASK"},
		{"tasks.start.tool", flagKeyTool, "TASKS_START_TOOL"},
		{"tasks.start.model", flagKeyModel, "TASKS_START_MODEL"},
		{
			"tasks.start.interactive", flagKeyInteractive,
			"TASKS_START_INTERACTIVE",
		},
		{"tasks.start.yes", "yes", "TASKS_START_YES"},
		{
			"tasks.start.plan_requires_approval", "plan-requires-approval",
			"TASKS_START_PLAN_REQUIRES_APPROVAL",
		},
	}
	for _, b := range bindings {
		_ = viper.BindPFlag(b.key, cmd.Flags().Lookup(b.flag))
		_ = viper.BindEnv(b.key, b.env)
	}
}
