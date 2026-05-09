package testcases_test

import (
	"context"
	"testing"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store/tasks"
)

// orderCapturingUI records the labels passed into SelectSource
// so the test can assert the exact option list and order.
type orderCapturingUI struct {
	labels []string // raw string values presented to the user
}

func (u *orderCapturingUI) SelectSource(
	_ context.Context, allowed []picker.Source,
) (picker.Source, error) {
	for _, s := range allowed {
		u.labels = append(u.labels, string(s))
	}
	return picker.SourceMarkdown, nil
}

func (u *orderCapturingUI) PickMarkdownInCwd(
	context.Context,
) (string, error) {
	return "/tmp/test.md", nil
}

func (u *orderCapturingUI) PickTask(
	context.Context, string, []tasks.Task,
) (string, bool, error) {
	return "", false, nil
}

func (u *orderCapturingUI) PickLinearProject(
	context.Context, []linear.Project,
) (linear.Project, bool, error) {
	return linear.Project{}, false, nil
}

func (u *orderCapturingUI) PromptLinearAPIKey(
	context.Context, string,
) (string, bool, error) {
	return "", false, nil
}

func (u *orderCapturingUI) PickLinearIssue(
	context.Context, []linear.Issue,
) (linear.Issue, bool, error) {
	return linear.Issue{}, false, nil
}

// TestSourcePicker_OptionOrderLinearMarkdownTask asserts the
// top-level source-picker entries are presented as:
//
//	linear, markdown, task
//
// and that the first entry (linear) is the default highlight.
func TestSourcePicker_OptionOrderLinearMarkdownTask(t *testing.T) {
	ui := &orderCapturingUI{}
	_, err := picker.PickSource(
		t.Context(), ui,
		[]picker.Source{
			picker.SourceLinear,
			picker.SourceMarkdown,
			picker.SourceTask,
		},
		// listTasks=nil so SourceTask never fires;
		// we only need SelectSource captured.
		nil, nil,
	)
	if err != nil {
		t.Fatalf("PickSource: %v", err)
	}
	want := []string{"linear", "markdown", "task"}
	if len(ui.labels) != len(want) {
		t.Fatalf("labels = %v, want %v", ui.labels, want)
	}
	for i := range want {
		if ui.labels[i] != want[i] {
			t.Fatalf("labels[%d] = %q, want %q",
				i, ui.labels[i], want[i])
		}
	}
	if ui.labels[0] != "linear" {
		t.Fatalf("first entry = %q, want %q (default highlight)",
			ui.labels[0], "linear")
	}
}
