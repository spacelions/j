package tasks

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

func persistStartRow(stderr io.Writer, target startTarget,
	agentLogPath string, pid int,
) {
	if target.IsNew {
		newStatus, err := tasks.Apply("", tasks.EventPlanBegin)
		if err != nil {
			panic("plan begin from zero value: " + err.Error())
		}
		t := tasks.Task{
			ID:            target.TaskID,
			Status:        newStatus,
			Summary:       tasks.Summary(target.Body, target.Source),
			PlanBeginAt:   time.Now().UTC(),
			AgentLogPath:  agentLogPath,
			BackgroundPID: pid,
			LinearIssue:   target.LinearIssue,
		}
		tasks.PersistWarn(stderr, t)
		tasks.Notify(tasks.Transition{
			From: "", Event: tasks.EventPlanBegin, To: newStatus,
		}, t)
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
	approvalSet := cmd.Flags().Changed("plan-requires-approval") || envSet("TASKS_START_PLAN_REQUIRES_APPROVAL")
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
