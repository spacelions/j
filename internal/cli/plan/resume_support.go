package plan

import (
	"io"
	"os"
	"time"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/store/tasks"
)

func readBestEffortWarn(stderr io.Writer, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		banner.DangerousBox(stderr, "J: read %s: %v", path, err)
		return ""
	}
	return string(data)
}

func planResumeBegin(existing tasks.Task) tasks.Task {
	t := existing
	t.Status = tasks.StatusPlanning
	t.PlanEndAt = nil
	if t.PlanBeginAt == nil {
		begin := time.Now().UTC()
		t.PlanBeginAt = &begin
	}
	return t
}

func planResumeFinish(t tasks.Task, runErr error, refinedRequirements, planMarkdown, target string) tasks.Task {
	end := time.Now().UTC()
	t.PlanEndAt = &end
	if runErr != nil {
		t.Status = tasks.StatusHelp
		return t
	}
	t.Status = tasks.StatusPlanDone
	t.Summary = tasks.Summary(tasks.PickSource(refinedRequirements, planMarkdown), target)
	return t
}
