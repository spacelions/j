package settings

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/store"
)

func runResetArgs(t *testing.T, in io.Reader, args ...string) (string, error) {
	t.Helper()
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if in == nil {
		in = &bytes.Buffer{}
	}
	cmd.SetIn(in)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String() + stderr.String(), err
}

// runResetDirect drives runReset / runResetFull / runResetTargets
// without going through the cobra root tree, so the shared pre-flight
// hook is bypassed. Tests use it to exercise the defense-in-depth
// branches that fire when artifacts are missing or corrupt.
func runResetDirect(t *testing.T, in io.Reader, args ...string) (string, error) {
	t.Helper()
	cmd := newResetCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	if in == nil {
		in = &bytes.Buffer{}
	}
	cmd.SetIn(in)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), err
}

// TestReset_Full_MissingJDir pins the `nothing to reset` defense
// branch: when .j is missing the full-reset path short-circuits.
// We bypass cobra so pre-flight does not heal the missing layout.
func TestReset_Full_MissingJDir(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := runResetDirect(t, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "nothing to reset") {
		t.Fatalf("stdout = %q", out)
	}
}

// TestReset_Full_EmptyJ pins the second defense branch: .j exists
// but settings does not. We bypass cobra so the partial layout
// reaches runReset instead of being completed by pre-flight.
func TestReset_Full_EmptyJ(t *testing.T) {
	t.Chdir(t.TempDir())
	jDir, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runResetDirect(t, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "nothing to reset") {
		t.Fatalf("stdout = %q, want nothing to reset", out)
	}
}

func TestReset_Full_YesRemovesJ(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "a.k=v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	jDir, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jDir); err != nil {
		t.Fatalf(".j: %v", err)
	}
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset", "-y")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out, "removed "+jDir) {
		t.Fatalf("stdout = %q, want line with %q", out, jDir)
	}
	if _, err := os.Stat(jDir); !os.IsNotExist(err) {
		t.Fatalf(".j should be gone, stat: %v", err)
	}
}

func TestReset_Full_StdinYes(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "a.k=v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	_, err := runResetArgs(t, bytes.NewBufferString("yes\n"), "reset")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	jDir, _ := store.DefaultDir()
	if _, err := os.Stat(jDir); !os.IsNotExist(err) {
		t.Fatal("expected .j removed")
	}
}

func TestReset_Full_StdinY(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "a.k=v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	_, err := runResetArgs(t, bytes.NewBufferString("y\n"), "reset")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	jDir, _ := store.DefaultDir()
	if _, err := os.Stat(jDir); !os.IsNotExist(err) {
		t.Fatal("expected .j removed")
	}
}

func TestReset_Full_StdinNo(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "a.k=v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := runResetArgs(t, bytes.NewBufferString("n\n"), "reset")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out, "reset aborted") {
		t.Fatalf("stdout = %q", out)
	}
	p, _ := store.DefaultPath()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("db should still exist: %v", err)
	}
}

// TestReset_Single_MissingDB pins the `nothing to reset` defense
// branch in runResetTargets when settings is missing. We bypass cobra
// so the missing-file state survives long enough to reach the branch.
func TestReset_Single_MissingDB(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := runResetDirect(t, &bytes.Buffer{}, "a.b")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "nothing to reset") {
		t.Fatalf("out = %q", out)
	}
}

func TestReset_Single_RemovesValue(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "b.k1=x"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := runSetArgs(t, "set", "b.k2=y"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset", "b.k1")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out, "unset b.k1") {
		t.Fatalf("out = %q", out)
	}
	p, _ := store.DefaultPath()
	s, err := store.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	_, ok, err := s.Get("b", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("k1 should be gone")
	}
	v, ok, err := s.Get("b", "k2")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || v != "y" {
		t.Fatalf("k2: got %q ok=%v", v, ok)
	}
}

func TestReset_Single_MissingKeyStillOK(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "b.k2=y"); err != nil {
		t.Fatalf("set: %v", err)
	}
	_, err := runResetArgs(t, &bytes.Buffer{}, "reset", "b.ghost")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
}

// TestReset_Single_BadKey pins the parse-error path for malformed
// bucket.key targets: a bare `.key` is rejected by parseBucketKey
// because the bucket portion is empty. (A bare `nodot` is now a
// valid bucket-level target — see TestReset_Bucket_RemovesAllKeys.)
func TestReset_Single_BadKey(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	_, err := runResetArgs(t, &bytes.Buffer{}, "reset", ".key")
	if err == nil {
		t.Fatal("expected error for empty bucket portion")
	}
}

// TestReset_Single_TrailingDot pins the second parse-error path:
// `bucket.` is rejected because the key portion is empty.
func TestReset_Single_TrailingDot(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	_, err := runResetArgs(t, &bytes.Buffer{}, "reset", "bucket.")
	if err == nil {
		t.Fatal("expected error for empty key portion")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read error") }

func TestReadConfirmationLine_ReadError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(errReader{})
	if _, err := readConfirmationLine(cmd); err == nil {
		t.Fatal("expected read error")
	}
}

func TestReadConfirmationLine_EmptyEOF(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(&bytes.Buffer{})
	got, err := readConfirmationLine(cmd)
	if err != nil {
		t.Fatalf("readConfirmationLine: %v", err)
	}
	if got != "" {
		t.Fatalf("readConfirmationLine = %q, want empty string", got)
	}
}

// TestRunResetTargets_StatError exercises the non-ENOENT stat error
// path: when .j is a regular file the settings stat fails. We bypass
// cobra so the corrupt layout reaches runResetTargets.
func TestRunResetTargets_StatError(t *testing.T) {
	t.Chdir(t.TempDir())
	d, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runResetDirect(t, &bytes.Buffer{}, "a.b"); err == nil {
		t.Fatal("expected error")
	}
}

// TestReset_Bucket_RemovesAllKeys pins the new bucket-level reset
// shape: every key under the bucket is gone afterwards and the CLI
// prints `unset <bucket>` exactly once.
func TestReset_Bucket_RemovesAllKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "planner.tool=cursor", "planner.model=opus"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset", "planner")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out, "unset planner\n") {
		t.Fatalf("out = %q, want unset planner line", out)
	}
	if strings.Contains(out, "unset planner.") {
		t.Fatalf("out = %q, bucket reset must not emit per-key lines", out)
	}
	p, _ := store.DefaultPath()
	s, err := store.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for _, k := range []string{"tool", "model"} {
		if _, ok, err := s.Get(store.BucketPlanner, k); err != nil {
			t.Fatalf("Get %s: %v", k, err)
		} else if ok {
			t.Fatalf("planner.%s should be gone", k)
		}
	}
	rows, err := s.List(store.BucketPlanner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("planner bucket should be empty, got %v", rows)
	}
}

// TestReset_Bucket_MissingBucketIsNoop pins the no-op success: when
// the named bucket was never written, `reset planner` still exits 0
// and prints `unset planner` (mirrors single-key missing-key reset).
func TestReset_Bucket_MissingBucketIsNoop(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset", "planner")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out, "unset planner\n") {
		t.Fatalf("out = %q, want unset planner line", out)
	}
}

// TestReset_MultiArg_Space pins the multi-arg shape: a whitespace-
// separated list of targets is processed in order, each emitting one
// `unset` line. Both a bucket target and a bucket.key target are
// applied.
func TestReset_MultiArg_Space(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "planner.tool=cursor", "planner.model=opus", "worker.model=sonnet"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset", "planner", "worker.model")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	idxBucket := strings.Index(out, "unset planner\n")
	idxKey := strings.Index(out, "unset worker.model\n")
	if idxBucket < 0 || idxKey < 0 || idxBucket > idxKey {
		t.Fatalf("out = %q, want `unset planner` before `unset worker.model`", out)
	}
	p, _ := store.DefaultPath()
	s, err := store.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	rows, err := s.List(store.BucketPlanner)
	if err != nil {
		t.Fatalf("List planner: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("planner not empty: %v", rows)
	}
	if _, ok, err := s.Get(store.BucketWorker, "model"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("worker.model should be gone")
	}
}

// TestReset_MultiArg_Mixed pins ordering for a bucket + key + bucket
// triple: every target is applied and lines are emitted left-to-right.
func TestReset_MultiArg_Mixed(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := runSetArgs(t, "set", "planner.tool=cursor", "worker.model=sonnet", "verifier.tool=cursor"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset", "planner", "worker.model", "verifier")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	idxPlanner := strings.Index(out, "unset planner\n")
	idxWorker := strings.Index(out, "unset worker.model\n")
	idxVerifier := strings.Index(out, "unset verifier\n")
	if idxPlanner < 0 || idxWorker < 0 || idxVerifier < 0 {
		t.Fatalf("missing line in out = %q", out)
	}
	if idxPlanner >= idxWorker || idxWorker >= idxVerifier {
		t.Fatalf("out = %q, want planner < worker.model < verifier", out)
	}
	p, _ := store.DefaultPath()
	s, err := store.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for _, b := range []string{store.BucketPlanner, store.BucketVerifier} {
		rows, err := s.List(b)
		if err != nil {
			t.Fatalf("List %s: %v", b, err)
		}
		if len(rows) != 0 {
			t.Fatalf("%s not empty: %v", b, rows)
		}
	}
	if _, ok, err := s.Get(store.BucketWorker, "model"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("worker.model should be gone")
	}
}

// TestReset_MultiArg_MissingDB pins the missing-DB short-circuit for
// the multi-arg path: with a non-empty target list and no .j layout
// the helper still exits 0 and prints `nothing to reset`. We bypass
// cobra so pre-flight does not heal the layout first.
func TestReset_MultiArg_MissingDB(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := runResetDirect(t, &bytes.Buffer{}, "planner", "worker.model")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out, "nothing to reset") {
		t.Fatalf("out = %q, want nothing to reset", out)
	}
}

// TestParseResetTargets_EmptyArg pins the empty-arg parse error.
// Cobra collapses adjacent whitespace so this branch only fires
// when callers (e.g. tests) drive runResetTargets directly.
// TestReset_Linear_APIKey pins that `j settings reset linear.api_key`
// removes the camelCase storage key (the user-typed snake form
// round-trips through storageKey).
func TestReset_Linear_APIKey(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketLinear, store.KeyLinearAPIKey, "lin_api_secret"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := runResetArgs(t, nil, "reset", "linear.api_key"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, ok, err := s.Get(store.BucketLinear, store.KeyLinearAPIKey); err != nil {
		t.Fatalf("Get: %v", err)
	} else if ok {
		t.Fatal("linear.apiKey was not removed by reset linear.api_key")
	}
}

func TestParseResetTargets_EmptyArg(t *testing.T) {
	if _, err := parseResetTargets([]string{""}); err == nil {
		t.Fatal("expected error for empty target")
	}
}
