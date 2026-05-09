package testcases_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/resolver"
	storetasks "github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/linear"
)

// recordingStartUI captures the `allowed` slice that
// resolver.ResolveStartTarget hands to the source picker so the
// test can assert the order. SelectSource short-circuits with a
// sentinel error so no sub-picker is exercised.
type recordingStartUI struct {
	captured []picker.Source
}

func (u *recordingStartUI) SelectSource(
	_ context.Context, allowed []picker.Source,
) (picker.Source, error) {
	u.captured = append([]picker.Source(nil), allowed...)
	return "", errors.New("stop")
}

func (u *recordingStartUI) PickMarkdownInCwd(
	context.Context,
) (string, error) {
	return "", nil
}

func (u *recordingStartUI) PickTask(
	context.Context, string, []storetasks.Task,
) (string, bool, error) {
	return "", false, nil
}

func (u *recordingStartUI) PromptLinearAPIKey(
	context.Context, string,
) (string, bool, error) {
	return "", false, nil
}

func (u *recordingStartUI) PickLinearProject(
	context.Context, []linear.Project,
) (linear.Project, bool, error) {
	return linear.Project{}, false, nil
}

func (u *recordingStartUI) PickLinearIssue(
	context.Context, []linear.Issue,
) (linear.Issue, bool, error) {
	return linear.Issue{}, false, nil
}

// TestTasksStart_SourcePickerListsLinearFirst pins the order of
// the source-picker entries that `j tasks start` (no flags) renders:
// linear, then markdown, then existing task. The first entry doubles
// as the highlighted default selection in the form widget, so this
// is the requirement from SPA-37 / the task plan.
//
// Black-box: drive resolver.ResolveStartTarget with a fake StartUI
// that captures the allowed slice and short-circuits so no real
// sub-picker fires.
func TestTasksStart_SourcePickerListsLinearFirst(t *testing.T) {
	ui := &recordingStartUI{}
	_, err := resolver.ResolveStartTarget(
		t.Context(), ui, bytes.NewBuffer(nil), "",
	)
	if err == nil {
		t.Fatal("expected sentinel error from fake SelectSource")
	}

	want := []picker.Source{
		picker.SourceLinear,
		picker.SourceMarkdown,
		picker.SourceTask,
	}
	if len(ui.captured) != len(want) {
		t.Fatalf("captured = %v, want %v", ui.captured, want)
	}
	for i, w := range want {
		if ui.captured[i] != w {
			t.Fatalf("captured[%d] = %q, want %q",
				i, ui.captured[i], w)
		}
	}
	if ui.captured[0] != picker.SourceLinear {
		t.Fatalf("first entry = %q, want %q (default highlight)",
			ui.captured[0], picker.SourceLinear)
	}
}

// TestTasksStart_FromFileBypassesPicker pins the documented
// out-of-scope shortcut: --from-file routes straight to the
// markdown loader without invoking the source picker, so the
// reorder cannot regress that path.
func TestTasksStart_FromFileBypassesPicker(t *testing.T) {
	ui := &recordingStartUI{}
	_, _ = resolver.ResolveStartTarget(
		t.Context(), ui, bytes.NewBuffer(nil),
		"/nonexistent/path-bypasses-picker.md",
	)
	if ui.captured != nil {
		t.Fatalf("picker should be bypassed; captured = %v",
			ui.captured)
	}
}
