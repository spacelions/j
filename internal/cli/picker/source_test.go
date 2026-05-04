package picker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// scriptedSourceUI is a SourceUI fake that pre-canned answers.
type scriptedSourceUI struct {
	source       Source
	sourceErr    error
	markdown     string
	markdownErr  error
	taskID       string
	taskOK       bool
	taskErr      error
	taskTitle    string
	mdCalls      int
	taskCalls    int
	sourceCalls  int
	allowedSeen  []Source
}

func (s *scriptedSourceUI) SelectSource(_ context.Context, allowed []Source) (Source, error) {
	s.sourceCalls++
	s.allowedSeen = append([]Source(nil), allowed...)
	if s.sourceErr != nil {
		return "", s.sourceErr
	}
	return s.source, nil
}

func (s *scriptedSourceUI) PickMarkdownInCwd(_ context.Context) (string, error) {
	s.mdCalls++
	return s.markdown, s.markdownErr
}

func (s *scriptedSourceUI) PickTask(_ context.Context, title string, _ []store.Task) (string, bool, error) {
	s.taskCalls++
	s.taskTitle = title
	if s.taskErr != nil {
		return "", false, s.taskErr
	}
	return s.taskID, s.taskOK, nil
}

func TestPickSource_Markdown(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceMarkdown, markdown: "/abs/feature.md"}
	res, err := PickSource(context.Background(), ui, []Source{SourceMarkdown, SourceLinear, SourceTask}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Source != SourceMarkdown || res.Markdown != "/abs/feature.md" || res.TaskID != "" || res.Cancelled {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickSource_Linear(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceLinear}
	res, err := PickSource(context.Background(), ui, []Source{SourceMarkdown, SourceLinear}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Source != SourceLinear || res.Markdown != "" || res.TaskID != "" || res.Cancelled {
		t.Fatalf("res = %+v", res)
	}
	if ui.mdCalls != 0 || ui.taskCalls != 0 {
		t.Fatalf("sub-pickers should not fire on linear: md=%d task=%d", ui.mdCalls, ui.taskCalls)
	}
}

func TestPickSource_Task_HappyPath(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask, taskID: "01ABC", taskOK: true}
	listTasks := func() ([]store.Task, error) {
		return []store.Task{{ID: "01ABC", Status: store.StatusPlanDone, Summary: "x"}}, nil
	}
	res, err := PickSource(context.Background(), ui, []Source{SourceTask}, listTasks, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Source != SourceTask || res.TaskID != "01ABC" || res.Cancelled {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(ui.taskTitle, "re-plan") {
		t.Fatalf("taskTitle = %q, want to mention re-plan", ui.taskTitle)
	}
}

func TestPickSource_Task_UserCancelled(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask, taskOK: false}
	listTasks := func() ([]store.Task, error) {
		return []store.Task{{ID: "01ABC"}}, nil
	}
	res, err := PickSource(context.Background(), ui, []Source{SourceTask}, listTasks, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Source != SourceTask || !res.Cancelled || res.TaskID != "" {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickSource_Task_NilListTasks(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask}
	_, err := PickSource(context.Background(), ui, []Source{SourceTask}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "listTasks callback") {
		t.Fatalf("err = %v, want listTasks misuse", err)
	}
}

func TestPickSource_Task_EmptyList(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask}
	listTasks := func() ([]store.Task, error) { return nil, nil }
	_, err := PickSource(context.Background(), ui, []Source{SourceTask}, listTasks, nil)
	if err == nil || !strings.Contains(err.Error(), "no tasks") {
		t.Fatalf("err = %v, want 'no tasks'", err)
	}
}

func TestPickSource_Task_EmptyList_CustomError(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask}
	listTasks := func() ([]store.Task, error) { return nil, nil }
	want := errors.New("plan: no tasks to re-plan; run `j plan` first")
	_, err := PickSource(context.Background(), ui, []Source{SourceTask}, listTasks, want)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want supplied empty-tasks error", err)
	}
}

func TestPickSource_Task_ListError(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask}
	want := errors.New("list boom")
	listTasks := func() ([]store.Task, error) { return nil, want }
	_, err := PickSource(context.Background(), ui, []Source{SourceTask}, listTasks, nil)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapped 'list boom'", err)
	}
}

func TestPickSource_SelectSourceError(t *testing.T) {
	want := errors.New("source boom")
	ui := &scriptedSourceUI{sourceErr: want}
	_, err := PickSource(context.Background(), ui, []Source{SourceMarkdown}, nil, nil)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapped 'source boom'", err)
	}
}

func TestPickSource_MarkdownError(t *testing.T) {
	want := errors.New("md boom")
	ui := &scriptedSourceUI{source: SourceMarkdown, markdownErr: want}
	_, err := PickSource(context.Background(), ui, []Source{SourceMarkdown}, nil, nil)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapped 'md boom'", err)
	}
}
