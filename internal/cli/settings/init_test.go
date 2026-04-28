package settings

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

func TestInit_CreatesDBAndBucket(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"init"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if !strings.Contains(stdout.String(), path) {
		t.Fatalf("stdout = %q, want path %q", stdout.String(), path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("db file not created: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("parent dir not created: %v", err)
	}

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open after init: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	buckets, err := s.ListBuckets()
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	if len(buckets) != 1 || buckets[0] != store.BucketPlanner {
		t.Fatalf("buckets = %v, want [%s]", buckets, store.BucketPlanner)
	}
}

func TestInit_Idempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	for i := 0; i < 2; i++ {
		cmd := New()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"init"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute (iteration %d): %v", i, err)
		}
	}

	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	buckets, err := s.ListBuckets()
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 1 || buckets[0] != store.BucketPlanner {
		t.Fatalf("buckets after rerun = %v", buckets)
	}
}

// TestInit_OpenError forces store.Open to fail by pre-creating the
// settings path as a directory.
func TestInit_OpenError(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected open error")
	}
}
