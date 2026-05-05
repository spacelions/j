package resolver

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/store/tasks"
)

type sourceUI struct {
	source picker.Source
	md     string
	taskID string
	ok     bool
	err    error
}

func (u sourceUI) SelectSource(context.Context, []picker.Source) (picker.Source, error) {
	return u.source, u.err
}

func (u sourceUI) PickMarkdownInCwd(context.Context) (string, error) {
	return u.md, u.err
}

func (u sourceUI) PickTask(context.Context, string, []tasks.Task) (string, bool, error) {
	return u.taskID, u.ok, u.err
}

func TestResolveStartTargetFromFile(t *testing.T) {
	setupResolverProject(t)
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("# task"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := ResolveStartTarget(context.Background(), sourceUI{}, bytes.NewBuffer(nil), path)
	if err != nil {
		t.Fatalf("ResolveStartTarget: %v", err)
	}
	if !target.IsNew || target.Body != "# task" || target.Source != path {
		t.Fatalf("target = %+v", target)
	}
}

func TestResolveStartTargetSources(t *testing.T) {
	setupResolverProject(t)
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := ResolveStartTarget(context.Background(), sourceUI{source: picker.SourceMarkdown, md: path}, bytes.NewBuffer(nil), "")
	if err != nil || !target.IsNew || target.Body != "body" {
		t.Fatalf("markdown target = %+v, %v", target, err)
	}

	seedResolverTask(t, tasks.Task{ID: "existing", Status: tasks.StatusPlanDone}, "plan", "")
	target, err = ResolveStartTarget(context.Background(), sourceUI{source: picker.SourceTask, taskID: "existing", ok: true}, bytes.NewBuffer(nil), "")
	if err != nil || target.TaskID != "existing" || target.IsNew {
		t.Fatalf("task target = %+v, %v", target, err)
	}

	var stdout bytes.Buffer
	target, err = ResolveStartTarget(context.Background(), sourceUI{source: picker.SourceLinear}, &stdout, "")
	if err != nil || target.TaskID != "" || !strings.Contains(stdout.String(), "J: tasks linear source") {
		t.Fatalf("linear target = %+v, stdout=%q err=%v", target, stdout.String(), err)
	}
}

func TestResolveStartTargetErrorsAndCancel(t *testing.T) {
	setupResolverProject(t)
	_, err := ResolveStartTarget(context.Background(), sourceUI{err: errors.New("select failed")}, bytes.NewBuffer(nil), "")
	if err == nil || !strings.Contains(err.Error(), "select failed") {
		t.Fatalf("select err = %v", err)
	}

	seedResolverTask(t, tasks.Task{ID: "existing", Status: tasks.StatusPlanDone}, "plan", "")
	target, err := ResolveStartTarget(context.Background(), sourceUI{source: picker.SourceTask, ok: false}, bytes.NewBuffer(nil), "")
	if err != nil || target.TaskID != "" {
		t.Fatalf("cancel target = %+v, %v", target, err)
	}

	_, err = ResolveStartTarget(context.Background(), sourceUI{source: picker.Source("bad")}, bytes.NewBuffer(nil), "")
	if err == nil || !strings.Contains(err.Error(), "unsupported source") {
		t.Fatalf("bad source err = %v", err)
	}
}
