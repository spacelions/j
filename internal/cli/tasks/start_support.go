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
)

func persistStartRow(stderr io.Writer, target startTarget, agentLogPath string, pid int) {
	if target.IsNew {
		begin := time.Now().UTC()
		store.PersistWarn(stderr, store.Task{
			ID:            target.TaskID,
			Status:        store.StatusPlanning,
			Summary:       store.Summary(target.Body, target.Source),
			PlanBeginAt:   &begin,
			AgentLogPath:  agentLogPath,
			BackgroundPID: pid,
		})
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
		return "", fmt.Errorf("J: resolve j binary: %w", err)
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
	if o.Selector == nil {
		o.Selector = picker.New(o.Stdin, o.Stderr)
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
