package plan

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/banner"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/store"
)

func readBestEffortWarn(stderr io.Writer, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		banner.DangerousFprintf(stderr, "J: warning: read %s: %v\n", path, err)
		return ""
	}
	return string(data)
}

func planResumeBegin(existing store.Task) store.Task {
	task := existing
	task.Status = store.StatusPlanning
	task.PlanEndAt = nil
	if task.PlanBeginAt == nil {
		begin := time.Now().UTC()
		task.PlanBeginAt = &begin
	}
	return task
}

func planResumeFinish(task store.Task, runErr error, refinedRequirements, planMarkdown, target string) store.Task {
	end := time.Now().UTC()
	task.PlanEndAt = &end
	if runErr != nil {
		task.Status = store.StatusHelp
		return task
	}
	task.Status = store.StatusPlanDone
	task.Summary = store.Summary(store.PickSource(refinedRequirements, planMarkdown), target)
	return task
}

func newResumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a previously started plan session",
		Long: "Lists tasks whose plan session is non-empty and resumes the chosen one " +
			"using the tool/model recorded on the task row. " +
			"Pass --from-task <id> (or PLAN_RESUME_FROM_TASK) to skip the picker. " +
			"With no eligible sessions, prints `J: there are no resumable sessions` " +
			"and exits 0. " +
			"Resume always runs interactive; the planner bucket's `interactive` " +
			"value is ignored.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunResume(cmd.Context(), ResumeOptions{
				TaskID: viper.GetString("plan.resume.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().String("from-task", "", "Resume the named task without showing the picker")
	_ = viper.BindPFlag("plan.resume.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("plan.resume.from_task", "PLAN_RESUME_FROM_TASK")
	return cmd
}
