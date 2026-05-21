package testutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// SeedAgentBucket pre-populates a bbolt bucket with tool / model so
// plan / work / verify treat the bucket as already configured.
func SeedAgentBucket(t *testing.T, bucket, tool, model string) {
	t.Helper()
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("testutil: Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.EnsureBucket(bucket); err != nil {
		t.Fatalf("testutil: EnsureBucket: %v", err)
	}
	for _, kv := range [][2]string{
		{"tool", tool},
		{"model", model},
	} {
		if err := s.Put(bucket, kv[0], kv[1]); err != nil {
			t.Fatalf("testutil: Put %s: %v", kv[0], err)
		}
	}
}

// SeedFullTask writes a task row plus requirements.md and plan.md
// files. The mutate hook lets each test override fields.
func SeedFullTask(t *testing.T, mutate func(*tasks.Task)) string {
	t.Helper()
	id := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("testutil: EnsureTaskDir: %v", err)
	}
	if err := WriteFile(
		filepath.Join(taskDir, tasks.RequirementsFileName),
		"# req\nbody",
	); err != nil {
		t.Fatalf("testutil: write requirements: %v", err)
	}
	if err := WriteFile(
		filepath.Join(taskDir, tasks.PlanFileName), "1. step\n",
	); err != nil {
		t.Fatalf("testutil: write plan: %v", err)
	}
	begin := time.Now().UTC().Add(-2 * time.Hour)
	end := begin.Add(time.Hour)
	task := tasks.Task{
		ID:                id,
		Status:            tasks.StatusPlanDone,
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		WorkTool:          "cursor",
		WorkModel:         "sonnet-4",
		VerifyTool:        "cursor",
		VerifyModel:       "sonnet-4",
		PlanResumeSession: "plan-cursor",
		Summary:           "seed",
		PlanBeginAt:       begin,
		PlanEndAt:         end,
	}
	if mutate != nil {
		mutate(&task)
	}
	SeedTaskRow(t, task)
	return id
}

// SeedAgentBucketToolModel writes only tool and model. Used by tests
// that want to make the role-bucket contract explicit at the call
// site.
func SeedAgentBucketToolModel(t *testing.T, bucket, tool, model string) {
	t.Helper()
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("testutil: Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.EnsureBucket(bucket); err != nil {
		t.Fatalf("testutil: EnsureBucket: %v", err)
	}
	if err := s.Put(bucket, "tool", tool); err != nil {
		t.Fatalf("testutil: Put tool: %v", err)
	}
	if err := s.Put(bucket, "model", model); err != nil {
		t.Fatalf("testutil: Put model: %v", err)
	}
}

// ReadAgentBucket returns (tool, model, interactive) from the settings bucket.
func ReadAgentBucket(t *testing.T, bucket string) (string, string, string) {
	t.Helper()
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("testutil: Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	tool, _, _ := s.Get(bucket, "tool")
	model, _, _ := s.Get(bucket, "model")
	interactive, _, _ := s.Get(bucket, "interactive")
	return tool, model, interactive
}

// SeedTaskRow writes the supplied task to its per-task TOML file so
// plan / work / verify shell-out branches see the row they expect
// when invoked with TaskID set.
func SeedTaskRow(t *testing.T, row tasks.Task) {
	t.Helper()
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()
	if err := s.PutTask(row); err != nil {
		t.Fatalf("testutil: PutTask: %v", err)
	}
}

// ReadTaskRow loads a task by id, failing the test when the row is
// missing or unreadable.
func ReadTaskRow(t *testing.T, id string) tasks.Task {
	t.Helper()
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("testutil: GetTask: %v", err)
	}
	return got
}

// WriteFile is a tiny convenience wrapper around os.WriteFile with
// the 0o644 mode shared by every per-task artifact (requirements.md,
// plan.md, verifier_findings.md). It exists so the per-package
// agent_test.go files don't each spell out the mode.
func WriteFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

// SeedRawTaskFile writes raw bytes (typically malformed TOML) to
// `<.j/tasks>/<id>/task.toml`. Used by decode-error tests that need
// to plant a corrupted row without going through PutTask's encoder.
func SeedRawTaskFile(t *testing.T, id string, body []byte) {
	t.Helper()
	dir := tasks.DefaultDir()
	taskDir := filepath.Join(dir, id)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("testutil: mkdir: %v", err)
	}
	taskFile := filepath.Join(taskDir, tasks.TaskFileName)
	if err := os.WriteFile(taskFile, body, 0o644); err != nil {
		t.Fatalf("testutil: write task.toml: %v", err)
	}
}
