package testcases_test

import (
	"context"
	"testing"

	"github.com/spacelions/j/internal/cli/picker"
	storetasks "github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/linear"
)

// stubTaskSourceUI is a minimal picker.SourceUI that records the
// title passed to PickTask so the test can assert the sub-picker
// prompt wording.
type stubTaskSourceUI struct {
	taskTitle string
}

func (s *stubTaskSourceUI) SelectSource(
	_ context.Context, _ []picker.Source,
) (picker.Source, error) {
	return picker.SourceTask, nil
}

func (s *stubTaskSourceUI) PickTask(
	_ context.Context, title string, _ []storetasks.Task,
) (string, bool, error) {
	s.taskTitle = title
	return "stub-id", true, nil
}

func (s *stubTaskSourceUI) PickMarkdownInCwd(
	context.Context,
) (string, error) {
	return "", nil
}

func (s *stubTaskSourceUI) PickLinearProject(
	context.Context, []linear.Project,
) (linear.Project, bool, error) {
	return linear.Project{}, false, nil
}

func (s *stubTaskSourceUI) PromptLinearAPIKey(
	context.Context, string,
) (string, bool, error) { // satisfy picker.SourceUI
	return "", false, nil
}

func (s *stubTaskSourceUI) PickLinearIssue(
	context.Context, []linear.Issue,
) (linear.Issue, bool, error) {
	return linear.Issue{}, false, nil
}

// TestSourcePicker_SubPickerTitleUsesTask confirms the sub-picker
// title matches the renamed option label.  "Select a task" replaces
// the old "Select an existing task".
func TestSourcePicker_SubPickerTitleUsesTask(t *testing.T) {
	ui := &stubTaskSourceUI{}
	listTasks := func() ([]storetasks.Task, error) {
		return []storetasks.Task{
			{ID: "01TASK", Status: storetasks.StatusPlanDone, Summary: "x"},
		}, nil
	}
	// Allow all three sources; SelectSource returns SourceTask.
	_, err := picker.PickSource(
		t.Context(), ui,
		[]picker.Source{
			picker.SourceLinear,
			picker.SourceMarkdown,
			picker.SourceTask,
		},
		listTasks, nil,
	)
	if err != nil {
		t.Fatalf("PickSource: %v", err)
	}
	if ui.taskTitle != "Select a task" {
		t.Fatalf("sub-picker title = %q, want %q",
			ui.taskTitle, "Select a task")
	}
}
