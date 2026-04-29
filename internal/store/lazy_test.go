package store

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenDefault_Success(t *testing.T) {
	t.Chdir(t.TempDir())
	var stderr bytes.Buffer
	s, ok := OpenDefault(&stderr, BucketPlanner)
	if !ok {
		t.Fatalf("OpenDefault failed: %s", stderr.String())
	}
	t.Cleanup(func() { _ = s.Close() })
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty, got %q", stderr.String())
	}
	buckets, err := s.ListBuckets()
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 1 || buckets[0] != BucketPlanner {
		t.Fatalf("buckets = %v", buckets)
	}
}

// TestOpenDefault_OpenFails replaces the would-be DB path with a
// directory so bolt.Open errors. The helper must surface the warning
// and return ok=false.
func TestOpenDefault_OpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if s, ok := OpenDefault(&stderr, BucketPlanner); ok {
		_ = s.Close()
		t.Fatal("expected open to fail")
	}
	if !strings.Contains(stderr.String(), "settings db") {
		t.Fatalf("stderr = %q, want db warning", stderr.String())
	}
}

// TestOpenDefault_BucketFails passes an empty bucket name; bbolt
// rejects empty bucket names with ErrBucketNameRequired, which is
// the only realistic way to drive EnsureBucket failure.
func TestOpenDefault_BucketFails(t *testing.T) {
	t.Chdir(t.TempDir())
	var stderr bytes.Buffer
	if s, ok := OpenDefault(&stderr, ""); ok {
		_ = s.Close()
		t.Fatal("expected ensure bucket to fail")
	}
	if !strings.Contains(stderr.String(), "settings bucket") {
		t.Fatalf("stderr = %q, want bucket warning", stderr.String())
	}
}

func TestOpenTaskLog_Success(t *testing.T) {
	t.Chdir(t.TempDir())
	var stderr bytes.Buffer
	s, ok := OpenTaskLog(&stderr, BucketTasks)
	if !ok {
		t.Fatalf("OpenTaskLog failed: %s", stderr.String())
	}
	t.Cleanup(func() { _ = s.Close() })
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty, got %q", stderr.String())
	}
	buckets, err := s.ListBuckets()
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 1 || buckets[0] != BucketTasks {
		t.Fatalf("buckets = %v", buckets)
	}
}

// TestOpenTaskLog_OpenFails replaces the would-be DB path with a
// directory so bolt.Open errors. The helper must surface the warning
// and return ok=false.
func TestOpenTaskLog_OpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if s, ok := OpenTaskLog(&stderr, BucketTasks); ok {
		_ = s.Close()
		t.Fatal("expected open to fail")
	}
	if !strings.Contains(stderr.String(), "tasks db") {
		t.Fatalf("stderr = %q, want db warning", stderr.String())
	}
}

// TestOpenTaskLog_BucketFails passes an empty bucket name; bbolt
// rejects empty bucket names with ErrBucketNameRequired, exercising
// the EnsureBucket failure branch.
func TestOpenTaskLog_BucketFails(t *testing.T) {
	t.Chdir(t.TempDir())
	var stderr bytes.Buffer
	if s, ok := OpenTaskLog(&stderr, ""); ok {
		_ = s.Close()
		t.Fatal("expected ensure bucket to fail")
	}
	if !strings.Contains(stderr.String(), "tasks bucket") {
		t.Fatalf("stderr = %q, want bucket warning", stderr.String())
	}
}

// TestOpenTaskLog_LegacyTasksFile pins the friendly error path: a
// regular file at .j/tasks (the previous schema) blocks the new
// directory layout, so OpenTaskLog must surface a warning and return
// ok=false.
func TestOpenTaskLog_LegacyTasksFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jdir, "tasks"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if s, ok := OpenTaskLog(&stderr, BucketTasks); ok {
		_ = s.Close()
		t.Fatal("expected legacy file to block open")
	}
	if !strings.Contains(stderr.String(), "tasks dir") {
		t.Fatalf("stderr = %q, want tasks-dir warning", stderr.String())
	}
}

func TestPersistAgentSelection_NilStore(t *testing.T) {
	var stderr bytes.Buffer
	PersistAgentSelection(nil, &stderr, BucketPlanner, "cursor", "sonnet-4", true)
	if stderr.Len() != 0 {
		t.Fatalf("nil store should be silent, got %q", stderr.String())
	}
}

func TestPersistAgentSelection_HappyPath(t *testing.T) {
	s := openInTemp(t)
	if err := s.EnsureBucket(BucketCoder); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	PersistAgentSelection(s, &stderr, BucketCoder, "cursor", "sonnet-4", false)
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	for k, want := range map[string]string{
		"tool":        "cursor",
		"model":       "sonnet-4",
		"interactive": "false",
	} {
		got, ok, err := s.Get(BucketCoder, k)
		if err != nil || !ok || got != want {
			t.Fatalf("Get(%s) = (%q,%v,%v) want %q", k, got, ok, err, want)
		}
	}
}

func TestPersistAgentSelection_PutErrorWarns(t *testing.T) {
	s := openInTemp(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	PersistAgentSelection(s, &stderr, BucketPlanner, "cursor", "sonnet-4", true)
	if !strings.Contains(stderr.String(), "warning: persist tool") {
		t.Fatalf("stderr = %q, want warning", stderr.String())
	}
}
