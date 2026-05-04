package plan

import (
	"io"
	"os"
	"time"

	"github.com/spacelions/j/internal/cli/banner"
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
