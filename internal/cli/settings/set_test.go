package settings

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

func TestSet_Table(t *testing.T) {
	cases := []struct {
		name      string
		arg       string
		wantOut   string
		wantErr   string
		wantStore string
	}{
		{
			name:      "valid",
			arg:       "planner.model=gpt-5",
			wantOut:   "set planner.model = gpt-5",
			wantStore: "gpt-5",
		},
		{
			name:      "key_with_dots",
			arg:       "planner.key.with.suffix=v",
			wantOut:   "set planner.key.with.suffix = v",
			wantStore: "v",
		},
		{
			name:      "value_with_equals",
			arg:       "foo.bar=a=b",
			wantOut:   "set foo.bar = a=b",
			wantStore: "a=b",
		},
		{
			name:      "empty_value",
			arg:       "foo.bar=",
			wantOut:   "set foo.bar = ",
			wantStore: "",
		},
		{
			name:    "no_equals",
			arg:     "nope",
			wantErr: "missing '='",
		},
		{
			name:    "empty_bucket",
			arg:     ".onlykey=v",
			wantErr: "non-empty",
		},
		{
			name:    "empty_key",
			arg:     "bucket.=v",
			wantErr: "non-empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			mustInit(t)
			out, err := runSetArgs(t, "set", tc.arg)
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
			// Confirm the literal stored value matches what we
			// asked for, including embedded '=' and empty strings.
			bucket, key, _, parseErr := parseKeyValue(tc.arg)
			if parseErr != nil {
				t.Fatalf("parseKeyValue helper failed: %v", parseErr)
			}
			path, err := store.DefaultPath()
			if err != nil {
				t.Fatalf("DefaultPath: %v", err)
			}
			s, err := store.Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })
			got, ok, err := s.Get(bucket, key)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if !ok {
				t.Fatalf("Get(%s.%s): missing", bucket, key)
			}
			if got != tc.wantStore {
				t.Fatalf("stored value = %q, want %q", got, tc.wantStore)
			}
		})
	}
}

// TestSet_PostInitWritesValue confirms that, after the new `j init`
// has laid down the layout, `j settings set` writes a value into the
// existing settings DB. This replaces the older lazy-creation test:
// pre-flight is what creates the file; set just writes to it.
func TestSet_PostInitWritesValue(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	out, err := runSetArgs(t, "set", "mybucket.key=hello")
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
		t.Fatalf("db not present: %v", err)
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

// runSetDirect drives runSet without going through the cobra root
// tree, so the shared pre-flight hook is bypassed. Tests use it to
// exercise defensive branches (e.g. settings path is a directory).
func runSetDirect(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newSetCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), err
}

// TestSet_OpenFails forces store.Open to fail by replacing the
// settings path with a directory. We bypass cobra so the corrupt
// layout reaches runSet instead of being healed by pre-flight.
func TestSet_OpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runSetDirect(t, "a.b=v"); err == nil {
		t.Fatal("expected error when path is a directory")
	}
}
