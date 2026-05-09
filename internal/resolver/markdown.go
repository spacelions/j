package resolver

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/mdfile"
)

// StartTarget bundles the resolved input for `j tasks start`. IsNew
// signals that requirements.md still needs to be written (the markdown
// or Linear arms); existing-task arms leave it false. LinearIssue is
// the upstream identifier (`ENG-123`) when the source was Linear, so
// the lifecycle can stamp it onto the task row.
type StartTarget struct {
	TaskID      string
	IsNew       bool
	Body        string
	Source      string
	LinearIssue string
}

func NewStartTargetFromMarkdown(raw string) (StartTarget, error) {
	abs, err := mdfile.Resolve(raw)
	if err != nil {
		return StartTarget{}, err
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return StartTarget{}, fmt.Errorf("read source: %w", err)
	}
	return StartTarget{
		TaskID: tasks.NewTaskID(),
		IsNew:  true,
		Body:   string(body),
		Source: abs,
	}, nil
}

// NewStartTargetFromBody mints an in-memory StartTarget for sources
// that don't have a markdown file on disk (the Linear flow).
// PrepareStartTaskFiles writes the body unchanged to
// `<taskDir>/requirements.md`. sourceLabel is recorded on the task
// row so `j tasks` can show the issue identifier instead of a path.
// linearIssue is the upstream `<TEAM>-<NUM>` form, propagated into
// the row's linear_issue column.
func NewStartTargetFromBody(
	body, sourceLabel, linearIssue string,
) StartTarget {
	return StartTarget{
		TaskID:      tasks.NewTaskID(),
		IsNew:       true,
		Body:        body,
		Source:      sourceLabel,
		LinearIssue: linearIssue,
	}
}

func PrepareStartTaskFiles(target StartTarget) (string, error) {
	taskDir, err := tasks.EnsureDir(target.TaskID)
	if err != nil {
		return "", fmt.Errorf("ensure task dir: %w", err)
	}
	if target.IsNew {
		requirementsPath := filepath.Join(
			taskDir, tasks.RequirementsFileName,
		)
		if err := os.WriteFile(
			requirementsPath, []byte(target.Body), 0o644,
		); err != nil {
			return "", fmt.Errorf("stage requirements: %w", err)
		}
	}
	return filepath.Join(taskDir, tasks.AgentLogFileName), nil
}
