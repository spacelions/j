package deepseek

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAgent_Name(t *testing.T) {
	if got := New().Name(); got != "deepseek" {
		t.Fatalf("Name = %q, want %q", got, "deepseek")
	}
}

// TestNewResumeID_AlwaysEmpty pins the contract: deepseek-tui has no
// pre-run session-id binding flag, so NewResumeID always returns
// ("", nil) regardless of how many times it is called.
func TestNewResumeID_AlwaysEmpty(t *testing.T) {
	a := New()
	for range 3 {
		got, err := a.NewResumeID(t.Context())
		if err != nil {
			t.Fatalf("NewResumeID: %v", err)
		}
		if got != "" {
			t.Fatalf("NewResumeID = %q, want empty", got)
		}
	}
}

// TestListModels_StaticAliases pins the static picker list and asserts
// ListModels returns a fresh copy (callers must not be able to mutate
// the package state).
func TestListModels_StaticAliases(t *testing.T) {
	a := New()
	got, err := a.ListModels(t.Context())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := []string{"deepseek-v4-pro"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListModels = %v, want %v", got, want)
	}
	got[0] = "MUTATED"
	again, err := New().ListModels(t.Context())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if again[0] == "MUTATED" {
		t.Fatalf(
			"ListModels returned a shared slice — caller mutation leaked: %v",
			again,
		)
	}
}

// TestTopArgs pins the leading argv segment shared by every phase:
// `-w <workspace>` plus the optional `-r <id>` when ResumeChatID is
// non-empty.
func TestTopArgs(t *testing.T) {
	cases := []struct {
		name   string
		ws     string
		resume string
		want   []string
	}{
		{
			"fresh",
			"/tmp/ws",
			"",
			[]string{"-w", "/tmp/ws"},
		},
		{
			"resume",
			"/tmp/ws",
			"abc-id",
			[]string{"-w", "/tmp/ws", "-r", "abc-id"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := topArgs(tc.ws, tc.resume)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("topArgs(%q, %q) = %v, want %v",
					tc.ws, tc.resume, got, tc.want)
			}
		})
	}
}

// TestExecArgs pins the headless argv tail:
// `exec --model <m> --auto -- <prompt>`. The literal `--` separator
// guards against a leading `-` line in the prompt body being parsed
// as a flag (mirrors the claude regression guard).
func TestExecArgs(t *testing.T) {
	got := execArgs("deepseek-v4-pro", "do the thing")
	want := []string{
		"exec", "--model", "deepseek-v4-pro", "--auto", "--",
		"do the thing",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("execArgs = %v, want %v", got, want)
	}
}

// TestParseDoctor pins the JSON parsing logic for `doctor --json`.
func TestParseDoctor(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantOK     bool
		wantConfig bool
		wantSource string
	}{
		{
			"happy",
			`{"api_key":{"source":"keychain"},"config_present":true}`,
			true, true, "keychain",
		},
		{
			"logged-out-empty-source",
			`{"api_key":{"source":""},"config_present":true}`,
			true, true, "",
		},
		{
			"no-config",
			`{"api_key":{"source":"keychain"},"config_present":false}`,
			true, false, "keychain",
		},
		{"empty", "", false, false, ""},
		{"junk", "not json at all", false, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseDoctor(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.ConfigPresent != tc.wantConfig {
				t.Fatalf(
					"ConfigPresent = %v, want %v",
					got.ConfigPresent, tc.wantConfig,
				)
			}
			if got.APIKey.Source != tc.wantSource {
				t.Fatalf(
					"APIKey.Source = %q, want %q",
					got.APIKey.Source, tc.wantSource,
				)
			}
		})
	}
}

// writeSession is a tiny test helper: marshal a session envelope and
// drop it at <dir>/<name>.json so the scanner picks it up.
func writeSession(
	t *testing.T, dir, name, id, ws string, createdAt time.Time,
) {
	t.Helper()
	envelope := map[string]any{
		"metadata": map[string]any{
			"id":         id,
			"workspace":  ws,
			"created_at": createdAt.UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
}

// TestScanSessions pins the workspace + since filter and the
// newest-first ordering. We populate three sessions: one outside the
// since window, one with the wrong workspace, and two valid ones.
// The scan must drop the first two and return the valid pair newest
// first.
func TestScanSessions(t *testing.T) {
	dir := t.TempDir()
	since := time.Now().Add(-1 * time.Hour)
	older := since.Add(-30 * time.Minute)
	mid := since.Add(10 * time.Minute)
	newer := since.Add(45 * time.Minute)

	writeSession(t, dir, "stale", "stale-id", "/ws/A", older)
	writeSession(t, dir, "wrong-ws", "wrong-id", "/ws/B", newer)
	writeSession(t, dir, "match-mid", "mid-id", "/ws/A", mid)
	writeSession(t, dir, "match-new", "new-id", "/ws/A", newer)
	// Non-JSON file in the dir: must be skipped, not crash.
	if err := os.WriteFile(
		filepath.Join(dir, "garbage.json"), []byte("not json"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	// Dot file with wrong extension: skipped via suffix filter.
	if err := os.WriteFile(
		filepath.Join(dir, "README.md"), []byte("ignore me"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	// Sub-directory: skipped via IsDir filter.
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
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
// machine: DEEPSEEK_HOME points at a tempdir with no `sessions/`
// child, so CaptureResumeID returns ("", nil) without error.
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
// matching entries and confirms CaptureResumeID returns the newer id.
func TestCaptureResumeID_PicksNewest(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envHome, home)
	dir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-time.Hour)
	writeSession(t, dir, "old", "old-id", "/ws/A",
		since.Add(5*time.Minute))
	writeSession(t, dir, "new", "newest-id", "/ws/A",
		since.Add(20*time.Minute))

	got, err := New().CaptureResumeID(t.Context(), "/ws/A", since)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != "newest-id" {
		t.Fatalf("got %q, want %q", got, "newest-id")
	}
}

// TestCaptureResumeID_NoMatch pins the empty-but-exists branch: the
// store has sessions but none for our workspace+since. Must return
// ("", nil).
func TestCaptureResumeID_NoMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envHome, home)
	dir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSession(t, dir, "other", "other-id", "/ws/B", time.Now())

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

// TestScanSessions_SkipsCorruptAndEmpty pins the resilient-scanner
// contract: a JSON file whose metadata.id is missing is treated as
// a miss, not a fatal error. The valid sibling still wins.
func TestScanSessions_SkipsCorruptAndEmpty(t *testing.T) {
	dir := t.TempDir()
	since := time.Now().Add(-time.Hour)
	// File with valid JSON but missing metadata.id — must be skipped.
	if err := os.WriteFile(
		filepath.Join(dir, "no-id.json"),
		[]byte(`{"metadata":{"workspace":"/ws/A","created_at":"`+
			time.Now().UTC().Format(time.RFC3339)+`"}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeSession(t, dir, "good", "good-id", "/ws/A", time.Now())
	got, err := scanSessions(dir, "/ws/A", since)
	if err != nil {
		t.Fatalf("scanSessions: %v", err)
	}
	if len(got) != 1 || got[0].ID != "good-id" {
		t.Fatalf("got %+v, want one good-id", got)
	}
}

func TestDecodeSession_ReadError(t *testing.T) {
	_, ok := decodeSession(filepath.Join(t.TempDir(), "missing.json"))
	if ok {
		t.Fatal("decodeSession ok = true, want false")
	}
}

// TestCaptureResumeID_HomeError pins the sessionsDir error branch:
// when neither DEEPSEEK_HOME nor a usable $HOME is available,
// CaptureResumeID surfaces the error so the caller can warn.
func TestCaptureResumeID_HomeError(t *testing.T) {
	t.Setenv(envHome, "")
	t.Setenv("HOME", "")
	// On darwin/linux, an empty $HOME makes os.UserHomeDir return an
	// error. CaptureResumeID propagates that wrapped err.
	_, err := New().CaptureResumeID(t.Context(), "/ws/A", time.Now())
	if err == nil {
		t.Skip("os.UserHomeDir tolerated empty HOME on this platform")
	}
}

// TestSessionsDir_HomeOverride pins DEEPSEEK_HOME wins over the
// default $HOME-based path.
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

// TestFormatLog_Identity pins the deepseek formatter contract: every
// input line passes through unchanged. The TUI prints plain human
// text rather than stream-json so there is nothing to render — the
// method exists only to satisfy the codingagents.Agent interface.
func TestFormatLog_Identity(t *testing.T) {
	a := New()
	cases := [][]byte{
		nil,
		{},
		[]byte("\n"),
		[]byte("plain log line\n"),
		[]byte(`{"type":"unused"}` + "\n"),
		[]byte("\xff\xfe binary bytes \x00 mid line"),
	}
	for _, in := range cases {
		got := a.FormatLog(in)
		if string(got) != string(in) {
			t.Fatalf("FormatLog(%q) = %q, want passthrough", in, got)
		}
	}
}

// TestSessionsDir_FallsBackToUserHome pins the no-override branch:
// when DEEPSEEK_HOME is unset, sessionsDir derives the path from
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
	want := filepath.Join(home, ".deepseek", "sessions")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
