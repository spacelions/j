package testcases_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

func TestTaskToml_SectionedLayoutBlackBox(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	now := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	id := tasks.NewTaskID()
	writeTaskRow(t, tasks.Task{
		ID:                  id,
		Status:              tasks.StatusCompleted,
		Summary:             "sectioned black box",
		PlanTool:            "codex",
		PlanModel:           "gpt-5",
		WorkTool:            "claude",
		WorkModel:           "opus",
		VerifyTool:          "cursor",
		VerifyModel:         "sonnet",
		Worktree:            "wt-sectioned",
		PlanResumeSession:   "plan-session",
		WorkResumeSession:   "work-session",
		VerifyResumeSession: "verify-session",
		PlanBeginAt:         now,
		PlanEndAt:           now.Add(time.Minute),
		WorkBeginAt:         now.Add(2 * time.Minute),
		WorkEndAt:           now.Add(3 * time.Minute),
		VerifyBeginAt:       now.Add(4 * time.Minute),
		VerifyEndAt:         now.Add(5 * time.Minute),
		DoneAt:              now.Add(6 * time.Minute),
		LinearIssue:         "SPA-124",
		PullRequestURL:      "https://example.test/pull/124",
		AgentLogPath:        "/tmp/task-agent.log",
	})

	root := decodeTaskToml(t, readTaskToml(t, id))
	requireRootSections(t, root)
	requireSectionKeys(t, section(t, root, "metadata"),
		"id", "status", "summary", "done_at")
	requireSectionKeys(t, section(t, root, "planner"),
		"tool", "model", "resume_session", "begin_at", "end_at")
	requireSectionKeys(t, section(t, root, "worker"),
		"tool", "model", "resume_session", "begin_at", "end_at",
		"worktree")
	requireSectionKeys(t, section(t, root, "verifier"),
		"tool", "model", "resume_session", "begin_at", "end_at")
	requireSectionKeys(t, section(t, root, "linear"), "issue")
	requireSectionKeys(t, section(t, root, "github"), "pull_request_url")
	requireSectionKeys(t, section(t, root, "logs"), "agent_log_path")

	emptyID := tasks.NewTaskID()
	writeTaskRow(t, tasks.Task{ID: emptyID, Status: tasks.StatusPlanning})
	emptyRoot := decodeTaskToml(t, readTaskToml(t, emptyID))
	for _, name := range []string{"planner", "worker", "verifier"} {
		sec := section(t, emptyRoot, name)
		requireNoSectionKeys(t, sec, "tool", "model", "resume_session")
	}
	requireNoSectionKeys(t, section(t, emptyRoot, "worker"), "worktree")
	requireNoSectionKeys(t, section(t, emptyRoot, "linear"), "issue")
	requireNoSectionKeys(t, section(t, emptyRoot, "github"),
		"pull_request_url")
	requireNoSectionKeys(t, section(t, emptyRoot, "logs"),
		"agent_log_path")

	got := readTaskRow(t, emptyID)
	if !got.PlanBeginAt.IsZero() || !got.WorkBeginAt.IsZero() ||
		!got.VerifyBeginAt.IsZero() || !got.DoneAt.IsZero() {
		t.Fatalf("unset timestamps loaded as populated: %+v", got)
	}
}

func writeTaskRow(t *testing.T, row tasks.Task) {
	t.Helper()
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()
	if err := s.PutTask(row); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
}

func readTaskRow(t *testing.T, id string) tasks.Task {
	t.Helper()
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return row
}

func readTaskToml(t *testing.T, id string) string {
	t.Helper()
	data, err := os.ReadFile(
		filepath.Join(tasks.DefaultDir(), id, tasks.TaskFileName))
	if err != nil {
		t.Fatalf("read task.toml: %v", err)
	}
	return string(data)
}

func decodeTaskToml(t *testing.T, body string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := toml.Unmarshal([]byte(body), &root); err != nil {
		t.Fatalf("decode task.toml: %v\n%s", err, body)
	}
	return root
}

func requireRootSections(t *testing.T, root map[string]any) {
	t.Helper()
	want := []string{
		"metadata", "planner", "worker", "verifier",
		"linear", "github", "logs",
	}
	if len(root) != len(want) {
		t.Fatalf("root keys = %v, want only %v", mapKeys(root), want)
	}
	for _, key := range want {
		if _, ok := root[key]; !ok {
			t.Fatalf("missing section %q in root keys %v", key, mapKeys(root))
		}
	}
}

func section(t *testing.T, root map[string]any, name string) map[string]any {
	t.Helper()
	sec, ok := root[name].(map[string]any)
	if !ok {
		t.Fatalf("section %q = %#v, want table", name, root[name])
	}
	return sec
}

func requireSectionKeys(t *testing.T, sec map[string]any, keys ...string) {
	t.Helper()
	if len(sec) != len(keys) {
		t.Fatalf("section keys = %v, want %v", mapKeys(sec), keys)
	}
	for _, key := range keys {
		if _, ok := sec[key]; !ok {
			t.Fatalf("missing key %q in section keys %v", key, mapKeys(sec))
		}
	}
}

func requireNoSectionKeys(t *testing.T, sec map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := sec[key]; ok {
			t.Fatalf("unexpected populated key %q in section %v", key, sec)
		}
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
