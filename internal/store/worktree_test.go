package store

import (
	"strings"
	"testing"
)

// TestWorktreeNameFor covers the slugify + fallback combinations so
// every branch (empty summary falling back to lowercased ULID, pure-
// punctuation summary, empty project, truncation) is exercised.
func TestWorktreeNameFor(t *testing.T) {
	cases := []struct {
		name    string
		project string
		task    Task
		want    string
	}{
		{
			name:    "summary-slugified",
			project: "j",
			task:    Task{ID: "01KQJEHKN55PNJN97SNRPZ6KGB", Summary: "Drop the legacy tasks file"},
			want:    "j-drop-the-legacy-tasks-file",
		},
		{
			name:    "summary-with-punctuation",
			project: "j",
			task:    Task{ID: "01ABC", Summary: "Fix R1: Remove `ErrLegacyTasksFile`!"},
			want:    "j-fix-r1-remove-errlegacytasksfile",
		},
		{
			name:    "empty-summary-falls-back-to-lower-id",
			project: "j",
			task:    Task{ID: "01KQJEHKN55PNJN97SNRPZ6KGB"},
			want:    "j-01kqjehkn55pnjn97snrpz6kgb",
		},
		{
			name:    "pure-punctuation-summary-falls-back-to-id",
			project: "j",
			task:    Task{ID: "01ABC", Summary: "!!! ??? ..."},
			want:    "j-01abc",
		},
		{
			name:    "empty-project-yields-task-only",
			project: "",
			task:    Task{ID: "01ABC", Summary: "hello"},
			want:    "hello",
		},
		{
			name:    "empty-project-and-empty-summary",
			project: "",
			task:    Task{ID: "01ABC"},
			want:    "01abc",
		},
		{
			name:    "empty-project-slug-and-empty-task-slug",
			project: "!!!",
			task:    Task{ID: "!!!"},
			want:    "",
		},
		{
			name:    "both-slugs-empty-after-slugify",
			project: "!!!",
			task:    Task{ID: "", Summary: "???"},
			want:    "",
		},
		{
			name:    "project-only-when-task-slug-empty",
			project: "my-proj",
			task:    Task{ID: "", Summary: ""},
			want:    "my-proj",
		},
		{
			name:    "long-summary-is-clipped",
			project: "j",
			task:    Task{ID: "01ABC", Summary: strings.Repeat("abcdefghij ", 10)},
			want:    "j-abcdefghij-abcdefghij-abcdefghij-abcdefghij-abcd",
		},
		{
			name:    "long-project-is-clipped-too",
			project: strings.Repeat("a", 60),
			task:    Task{ID: "01ABC", Summary: "sum"},
			want:    strings.Repeat("a", 48) + "-sum",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WorktreeNameFor(tc.project, tc.task); got != tc.want {
				t.Fatalf("WorktreeNameFor(%q, %+v) = %q, want %q", tc.project, tc.task, got, tc.want)
			}
		})
	}
}

// TestPutGetTask_WorktreeRoundTrip pins AC for R2: a PutTask ->
// GetTask round trip preserves a non-empty Worktree byte-identically.
func TestPutGetTask_WorktreeRoundTrip(t *testing.T) {
	s := openTaskStore(t)
	in := Task{
		ID:       "id-wt",
		Status:   StatusWorking,
		Summary:  "hello",
		Worktree: "j-my-task",
	}
	if err := s.PutTask(in); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	out, err := s.GetTask("id-wt")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if out.Worktree != "j-my-task" {
		t.Fatalf("Worktree round-trip = %q, want %q", out.Worktree, "j-my-task")
	}
}
