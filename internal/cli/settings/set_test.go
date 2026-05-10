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
			name:    "missing_dot",
			arg:     "bucket=v",
			wantErr: "missing '.'",
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

// TestSet_MultiplePairs verifies that two valid pairs are written in
// order and that both lines appear in stdout.
func TestSet_MultiplePairs(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	out, err := runSetArgs(t, "set", "planner.tool=cursor", "planner.model=opus")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	first := strings.Index(out, "set planner.tool = cursor")
	second := strings.Index(out, "set planner.model = opus")
	if first < 0 || second < 0 {
		t.Fatalf("stdout = %q, want both set lines", out)
	}
	if first > second {
		t.Fatalf("stdout = %q, want first pair printed before second", out)
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
	for _, want := range []struct{ k, v string }{
		{"tool", "cursor"},
		{"model", "opus"},
	} {
		got, ok, err := s.Get("planner", want.k)
		if err != nil {
			t.Fatalf("Get(%s): %v", want.k, err)
		}
		if !ok {
			t.Fatalf("Get(planner.%s): missing", want.k)
		}
		if got != want.v {
			t.Fatalf("planner.%s = %q, want %q", want.k, got, want.v)
		}
	}
}

// TestSet_MultipleParseErrorBeforeWrites confirms that a parse error
// on any arg aborts the whole batch: no good pair is written, even
// the ones that appeared before the bad arg.
func TestSet_MultipleParseErrorBeforeWrites(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	_, err := runSetArgs(t, "set", "a.b=1", "bad-no-equals", "c.d=2")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing '='") {
		t.Fatalf("err = %v, want missing '='", err)
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
	for _, c := range []struct{ bucket, key string }{
		{"a", "b"},
		{"c", "d"},
	} {
		if _, ok, err := s.Get(c.bucket, c.key); err != nil {
			t.Fatalf("Get(%s.%s): %v", c.bucket, c.key, err)
		} else if ok {
			t.Fatalf("Get(%s.%s): unexpectedly present, batch should have aborted before any write", c.bucket, c.key)
		}
	}
}

// TestSet_Linear_APIKey_RoundTripBothForms confirms that the user
// can type either `linear.api_key` or `linear.api-key` and the
// value lands under the camelCase storage key. Reads back via the
// same kebab/snake form (rendered as `api_key` in `j settings`).
func TestSet_Linear_APIKey_RoundTripBothForms(t *testing.T) {
	cases := []string{"linear.api_key=lin_api_xx", "linear.api-key=lin_api_yy"}
	for _, arg := range cases {
		t.Run(arg, func(t *testing.T) {
			t.Chdir(t.TempDir())
			mustInit(t)
			if _, err := runSetArgs(t, "set", arg); err != nil {
				t.Fatalf("Execute: %v", err)
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
			got, ok, err := s.Get(store.BucketLinear, store.KeyLinearAPIKey)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if !ok {
				t.Fatalf("Get(%s.%s): missing", store.BucketLinear, store.KeyLinearAPIKey)
			}
			want := strings.SplitN(arg, "=", 2)[1]
			if got != want {
				t.Fatalf("stored value = %q, want %q", got, want)
			}
		})
	}
}

// TestSet_Linear_ProjectIsIdentityMapped confirms that
// `linear.project=...` writes to the identity-mapped storage key.
func TestSet_Linear_ProjectIsIdentityMapped(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "linear.project=p123"); err != nil {
		t.Fatalf("Execute: %v", err)
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
	got, ok, err := s.Get(store.BucketLinear, store.KeyLinearProject)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || got != "p123" {
		t.Fatalf("Get(linear.project) = %q, ok=%v", got, ok)
	}
}

// TestSet_DuplicateKeyLastWins confirms that when the same key is
// listed twice, the second assignment overwrites the first.
func TestSet_DuplicateKeyLastWins(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "a.b=1", "a.b=2"); err != nil {
		t.Fatalf("Execute: %v", err)
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
	got, ok, err := s.Get("a", "b")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get(a.b): missing")
	}
	if got != "2" {
		t.Fatalf("a.b = %q, want %q", got, "2")
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
