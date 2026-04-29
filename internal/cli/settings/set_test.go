package settings

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

func TestSet_Table(t *testing.T) {
	t.Chdir(t.TempDir())

	cases := []struct {
		name     string
		bucketKV string
		value    string
		wantOut  string
		wantErr  string
	}{
		{
			name:     "valid",
			bucketKV: "planner.model",
			value:    "gpt-5",
			wantOut:  "set planner.model = gpt-5",
		},
		{
			name:     "key_with_dots",
			bucketKV: "planner.key.with.suffix",
			value:    "v",
			wantOut:  "set planner.key.with.suffix = v",
		},
		{
			name:     "no_dot",
			bucketKV: "nope",
			value:    "x",
			wantErr:  "missing",
		},
		{
			name:     "empty_bucket",
			bucketKV: ".onlykey",
			value:    "v",
			wantErr:  "non-empty",
		},
		{
			name:     "empty_key",
			bucketKV: "bucket.",
			value:    "v",
			wantErr:  "non-empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			out, err := runSetArgs(t, "set", tc.bucketKV, tc.value)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Fatalf("stdout = %q, want %q", out, tc.wantOut)
			}
		})
	}
}

func TestSet_LazilyCreatesJ(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := runSetArgs(t, "set", "mybucket.key", "hello")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "set mybucket.key = hello") {
		t.Fatalf("stdout = %q", out)
	}
	p, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("db not created: %v", err)
	}
}

func runSetArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String() + stderr.String(), err
}

func TestSet_OpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = runSetArgs(t, "set", "a.b", "v")
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
}
