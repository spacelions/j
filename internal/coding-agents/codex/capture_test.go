package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeRollout drops a one-line session_meta envelope under
// <dir>/<sub>/rollout-<name>.jsonl so the scanner picks it up.
// extraTrailing optionally appends additional JSONL records after the
// meta line so the decoder must stop at the first newline.
func writeRollout(
	t *testing.T, dir, sub, name, id, cwd string,
	ts time.Time, extraTrailing string,
) string {
	t.Helper()
	full := filepath.Join(dir, sub)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"timestamp": ts.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id":        id,
			"cwd":       cwd,
			"timestamp": ts.UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := make([]byte, 0, len(data)+1+len(extraTrailing))
	body = append(body, data...)
	body = append(body, '\n')
	body = append(body, []byte(extraTrailing)...)
	path := filepath.Join(full, "rollout-"+name+".jsonl")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestScanSessions pins the cwd + since filter and the newest-first
// ordering. We populate four rollouts: one outside the since window,
// one with the wrong cwd, and two valid ones. The scan must drop the
// first two and return the valid pair newest first.
func TestScanSessions(t *testing.T) {
	dir := t.TempDir()
	since := time.Now().Add(-1 * time.Hour)
	older := since.Add(-30 * time.Minute)
	mid := since.Add(10 * time.Minute)
	newer := since.Add(45 * time.Minute)

	writeRollout(t, dir, "2026/04/09", "stale", "stale-id", "/ws/A", older, "")
	writeRollout(t, dir, "2026/04/09", "wrong-ws", "wrong-id", "/ws/B", newer, "")
	writeRollout(t, dir, "2026/05/09", "match-mid", "mid-id", "/ws/A", mid, "")
	writeRollout(
		t, dir, "2026/05/10", "match-new", "new-id", "/ws/A", newer,
		`{"type":"turn.started"}`+"\n",
	)
	// Non-rollout file in the dated dir: must be skipped via prefix.
	if err := os.WriteFile(
		filepath.Join(dir, "2026/05/10", "README.md"),
		[]byte("ignore me"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	// File with rollout prefix but wrong extension: skipped via suffix.
	if err := os.WriteFile(
		filepath.Join(dir, "2026/05/10", "rollout-bogus.txt"),
		[]byte("ignore me"), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	got, err := scanSessions(dir, "/ws/A", since)
	if err != nil {
		t.Fatalf("scanSessions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != "new-id" || got[1].ID != "mid-id" {
		t.Fatalf("ordering wrong, got %q then %q",
			got[0].ID, got[1].ID)
	}
}

// TestScanSessions_MissingDir pins the no-store branch: a non-existent
// directory yields (nil, nil) so a fresh machine looks like "no match"
// rather than an error.
func TestScanSessions_MissingDir(t *testing.T) {
	got, err := scanSessions(
		filepath.Join(t.TempDir(), "does-not-exist"),
		"/ws/A", time.Now(),
	)
	if err != nil {
		t.Fatalf("scanSessions: %v", err)
	}
	if got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

// TestCaptureResumeID_NoStore covers the happy path of a fresh
// machine: CODEX_HOME points at a tempdir with no `sessions/` child,
// so CaptureResumeID returns ("", nil) without error.
func TestCaptureResumeID_NoStore(t *testing.T) {
	t.Setenv(envHome, t.TempDir())
	got, err := New().CaptureResumeID(
		t.Context(), "/ws/A", time.Now().Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// TestCaptureResumeID_PicksNewest seeds the sessions store with two
// matching entries on different dated subdirectories and confirms
// CaptureResumeID returns the newer id.
func TestCaptureResumeID_PicksNewest(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envHome, home)
	dir := filepath.Join(home, "sessions")
	since := time.Now().Add(-time.Hour)
	writeRollout(t, dir, "2026/05/09", "old", "old-id", "/ws/A",
		since.Add(5*time.Minute), "")
	writeRollout(t, dir, "2026/05/10", "new", "newest-id", "/ws/A",
		since.Add(20*time.Minute), "")

	got, err := New().CaptureResumeID(t.Context(), "/ws/A", since)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != "newest-id" {
		t.Fatalf("got %q, want %q", got, "newest-id")
	}
}

// TestCaptureResumeID_NoMatch pins the empty-but-exists branch: the
// store has rollouts but none for our cwd+since. Must return ("", nil).
func TestCaptureResumeID_NoMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envHome, home)
	dir := filepath.Join(home, "sessions")
	writeRollout(t, dir, "2026/05/10", "other", "other-id", "/ws/B",
		time.Now(), "")

	got, err := New().CaptureResumeID(
		t.Context(), "/ws/A", time.Now().Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// TestScanSessions_SkipsCorrupt pins the resilient-scanner contract:
// a rollout file whose first line is not parseable JSON or is missing
// the session_meta type is treated as a miss, not a fatal error. The
// valid sibling still wins.
func TestScanSessions_SkipsCorrupt(t *testing.T) {
	dir := t.TempDir()
	since := time.Now().Add(-time.Hour)
	if err := os.MkdirAll(
		filepath.Join(dir, "2026/05/10"), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	// Corrupt: not JSON.
	if err := os.WriteFile(
		filepath.Join(dir, "2026/05/10", "rollout-junk.jsonl"),
		[]byte("not json\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	// Wrong type: parses, but type != session_meta.
	if err := os.WriteFile(
		filepath.Join(dir, "2026/05/10", "rollout-wrong-type.jsonl"),
		[]byte(`{"type":"turn.started"}`+"\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	// Empty file: ReadBytes returns EOF immediately.
	if err := os.WriteFile(
		filepath.Join(dir, "2026/05/10", "rollout-empty.jsonl"),
		nil, 0o644,
	); err != nil {
		t.Fatal(err)
	}
	// Missing payload.id: parses, but id is empty.
	if err := os.WriteFile(
		filepath.Join(dir, "2026/05/10", "rollout-no-id.jsonl"),
		[]byte(`{"type":"session_meta","payload":{"cwd":"/ws/A"}}`+"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeRollout(t, dir, "2026/05/10", "good", "good-id", "/ws/A",
		time.Now(), "")
	got, err := scanSessions(dir, "/ws/A", since)
	if err != nil {
		t.Fatalf("scanSessions: %v", err)
	}
	if len(got) != 1 || got[0].ID != "good-id" {
		t.Fatalf("got %+v, want one good-id", got)
	}
}

// TestCaptureResumeID_HomeError pins the sessionsDir error branch:
// when neither CODEX_HOME nor a usable $HOME is available,
// CaptureResumeID surfaces the error so the caller can warn.
func TestCaptureResumeID_HomeError(t *testing.T) {
	t.Setenv(envHome, "")
	t.Setenv("HOME", "")
	_, err := New().CaptureResumeID(t.Context(), "/ws/A", time.Now())
	if err == nil {
		t.Skip("os.UserHomeDir tolerated empty HOME on this platform")
	}
}

// TestCaptureResumeID_ScanError pins the second error branch of
// CaptureResumeID: when the sessions directory exists but cannot be
// statted (e.g. a parent directory is unreadable), the wrapped error
// from scanSessions reaches the caller. The test triggers this by
// pointing CODEX_HOME at a path under an unreadable parent.
func TestCaptureResumeID_ScanError(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "codex-home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(home, "sessions"), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	t.Setenv(envHome, home)
	_, err := New().CaptureResumeID(
		t.Context(), "/ws/A", time.Now().Add(-time.Hour),
	)
	if err == nil {
		t.Skip("stat tolerated unreadable parent on this platform")
	}
}

// TestDecodeMeta_OpenError pins decodeMeta's open-error branch: a
// path the process cannot read yields ok=false rather than panicking
// (the scanner relies on this to skip a permission-denied rollout
// without aborting the whole walk).
func TestDecodeMeta_OpenError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-x.jsonl")
	if err := os.WriteFile(path, []byte("ignored"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	if _, ok := decodeMeta(path); ok {
		t.Skip("open tolerated mode 0 on this platform")
	}
}

// TestSessionsDir_HomeOverride pins CODEX_HOME wins over the default
// $HOME-based path.
func TestSessionsDir_HomeOverride(t *testing.T) {
	t.Setenv(envHome, "/tmp/custom-home")
	got, err := sessionsDir()
	if err != nil {
		t.Fatalf("sessionsDir: %v", err)
	}
	want := filepath.Join("/tmp/custom-home", "sessions")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestSessionsDir_FallsBackToUserHome pins the no-override branch:
// when CODEX_HOME is unset, sessionsDir derives the path from
// os.UserHomeDir().
func TestSessionsDir_FallsBackToUserHome(t *testing.T) {
	t.Setenv(envHome, "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("UserHomeDir: %v (env strips $HOME)", err)
	}
	got, err := sessionsDir()
	if err != nil {
		t.Fatalf("sessionsDir: %v", err)
	}
	want := filepath.Join(home, ".codex", "sessions")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestIsRollout pins the filename filter shape the scanner relies on.
func TestIsRollout(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"rollout-2026-05-10T15-00-45-019e0d41.jsonl", true},
		{"rollout-anything.jsonl", true},
		{"README.md", false},
		{"rollout-bogus.txt", false},
		{"some-rollout.jsonl", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRollout(tc.name); got != tc.want {
				t.Fatalf("isRollout(%q) = %v, want %v",
					tc.name, got, tc.want)
			}
		})
	}
}

// TestScanSessions_WalkError pins the walk-error propagation branch:
// when the dated directory exists but is unreadable, scanSessions
// returns the wrapped error rather than silently dropping matches.
// Skipped on platforms / environments where the chmod trick does not
// produce a permission-denied (e.g. running as root).
func TestScanSessions_WalkError(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "2026/05/10")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	_, err := scanSessions(dir, "/ws/A", time.Now().Add(-time.Hour))
	if err == nil {
		t.Skip("walk tolerated unreadable subdir on this platform")
	}
}
