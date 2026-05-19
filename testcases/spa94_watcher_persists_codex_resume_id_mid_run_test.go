package testcases_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
)

// TestSPA94WatcherPersistsCodexResumeIDMidRun pins acceptance criteria
// AC1: while a codex worker is still running, the watcher must persist
// the backend's resume session id to the recorder as soon as the
// rollout's session_meta record lands on disk. The historical
// poll/timeout helper would miss this race when codex took longer
// than 2s to write its session_meta; the fsnotify-driven watcher must
// not.
//
// Black-box: drive WatchAndSaveActiveResumeID with the real codex
// backend, start the watcher with the test process's own pid so it
// observes a live pid, then write a rollout JSONL into the codex
// per-task scoped home AFTER the watcher is already running. The
// watcher should return the rollout's session id and record it on
// the supplied recorder.
func TestSPA94WatcherPersistsCodexResumeIDMidRun(t *testing.T) {
	taskDir := t.TempDir()
	since := time.Now().Add(-time.Minute)
	rolloutTS := since.Add(30 * time.Second)
	// Pre-create the dated rollout directory so the watcher attaches a
	// kqueue/inotify watch to it before the test writes the rollout
	// file. fsnotify's recursive coverage walks at startup; if the
	// dated subtree appears later the file-write event fires against a
	// dir fsnotify is not yet watching and the test races.
	rolloutDir := filepath.Join(
		taskDir, ".codex-home", "sessions",
		rolloutTS.UTC().Format("2006"),
		rolloutTS.UTC().Format("01"),
		rolloutTS.UTC().Format("02"),
	)
	if err := os.MkdirAll(rolloutDir, 0o755); err != nil {
		t.Fatalf("mkdir rollout dir: %v", err)
	}

	wantID := "01J0X8WATCHER-MID-RUN-IDXXXXX"

	recorder := &spa94Recorder{}
	capture := codingagents.ResumeCapture{
		TaskDir: taskDir,
		Since:   since,
		Stderr:  &bytes.Buffer{},
	}

	done := make(chan string, 1)
	go func() {
		done <- codingagents.WatchAndSaveActiveResumeID(
			t.Context(), codex.New(), recorder, capture, os.Getpid(),
		)
	}()

	// Give the watcher a beat to register its dir watches before the
	// filesystem write — otherwise the file-write event could land
	// before fsnotify has attached its watch to the dated directory.
	time.Sleep(250 * time.Millisecond)
	writeSPA94CodexRollout(t, taskDir, wantID, rolloutTS)

	select {
	case got := <-done:
		if got != wantID {
			t.Fatalf(
				"WatchAndSaveActiveResumeID = %q, want %q "+
					"(watcher must capture id after a mid-run "+
					"rollout write)", got, wantID,
			)
		}
	case <-time.After(5 * time.Second):
		t.Fatal(
			"WatchAndSaveActiveResumeID did not return within 5s " +
				"after a mid-run rollout write — fsnotify watcher " +
				"is not surfacing events to CaptureResumeID",
		)
	}
	if got := recorder.snapshot(); got != wantID {
		t.Fatalf(
			"recorder.RecordResumeSession = %q, want %q",
			got, wantID,
		)
	}
}

// spa94Recorder is a testcases-package-private recorder shared by the
// SPA-94 watcher tests. The mutex covers the watcher goroutine and
// the test driver reading the recorded id.
type spa94Recorder struct {
	mu sync.Mutex
	id string
}

func (r *spa94Recorder) RecordResumeSession(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.id = id
}

func (r *spa94Recorder) snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.id
}

func writeSPA94CodexRollout(
	t *testing.T, taskDir, id string, ts time.Time,
) {
	t.Helper()
	dir := filepath.Join(
		taskDir, ".codex-home", "sessions",
		ts.UTC().Format("2006"),
		ts.UTC().Format("01"),
		ts.UTC().Format("02"),
	)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	envelope := map[string]any{
		"timestamp": ts.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id":        id,
			"cwd":       taskDir,
			"timestamp": ts.UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	data = append(data, '\n')
	path := filepath.Join(dir, "rollout-"+id+".jsonl")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
}
