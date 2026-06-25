package codingagents

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAgent is a minimal Agent that does not implement
// ResumeIDCapturer. CaptureResumeID must short-circuit to ("", nil)
// for it.
type stubAgent struct{}

func (stubAgent) Name() string { return "stub" }

func (stubAgent) ListModels(_ context.Context) ([]string, error) {
	return nil, nil
}

func (stubAgent) CheckLogin(_ context.Context) error { return nil }

func (stubAgent) NewResumeID(_ context.Context) (string, error) {
	return "", nil
}

func (stubAgent) Plan(_ context.Context, _ PlanRequest) (int, error) {
	return 0, nil
}

func (stubAgent) Work(_ context.Context, _ WorkRequest) (int, error) {
	return 0, nil
}

func (stubAgent) Verify(
	_ context.Context, _ VerifyRequest,
) (int, error) {
	return 0, nil
}

func (stubAgent) FormatLog(line []byte) []byte { return line }

// capturingAgent embeds stubAgent and implements ResumeIDCapturer
// with a closure so tests can pin the call's return values.
type capturingAgent struct {
	stubAgent
	id  string
	err error
}

func (c capturingAgent) CaptureResumeID(
	_ context.Context, _ string, _ time.Time,
) (string, error) {
	return c.id, c.err
}

// delayedCapturingAgent returns "" until rollout.jsonl exists. The
// call counter is atomic so the watcher goroutine and the test driver
// can both observe it safely.
type delayedCapturingAgent struct {
	stubAgent
	calls atomic.Int32
}

func (d *delayedCapturingAgent) CaptureResumeID(
	_ context.Context, taskDir string, _ time.Time,
) (string, error) {
	d.calls.Add(1)
	_, err := os.Stat(filepath.Join(taskDir, "rollout.jsonl"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return "delayed", nil
}

// TestCaptureResumeID_NotImplemented pins the type-assertion
// short-circuit: agents that do not implement ResumeIDCapturer get
// ("", nil) without any side effect.
func TestCaptureResumeID_NotImplemented(t *testing.T) {
	got, err := CaptureResumeID(
		t.Context(), stubAgent{}, "/ws/A", time.Now(),
	)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestCaptureResumeID_Implemented pins the happy path: the
// implementer's return values are passed through verbatim.
func TestCaptureResumeID_Implemented(t *testing.T) {
	a := capturingAgent{id: "captured", err: nil}
	got, err := CaptureResumeID(
		t.Context(), a, "/ws/A", time.Now(),
	)
	require.NoError(t, err)
	assert.Equal(t, "captured", got)
}

// TestCaptureResumeID_ImplementedError pins the error pass-through.
func TestCaptureResumeID_ImplementedError(t *testing.T) {
	want := errors.New("scan blew up")
	a := capturingAgent{id: "", err: want}
	_, err := CaptureResumeID(
		t.Context(), a, "/ws/A", time.Now(),
	)
	require.ErrorIs(t, err, want)
}

type recordingResume struct {
	mu sync.Mutex
	id string
}

func (r *recordingResume) RecordResumeSession(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.id = id
}

func (r *recordingResume) snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.id
}

func TestCaptureAndSaveResumeID_RecordsCapturedID(t *testing.T) {
	recorder := &recordingResume{}
	got := CaptureAndSaveResumeID(
		t.Context(),
		capturingAgent{id: "captured"},
		recorder,
		ResumeCapture{
			TaskDir: "/ws/A",
			Since:   time.Now(),
			Stderr:  &bytes.Buffer{},
		},
	)
	assert.Equal(t, "captured", got)
	assert.Equal(t, "captured", recorder.id)
}

func TestCaptureAndSaveResumeID_WarnsOnCaptureError(t *testing.T) {
	recorder := &recordingResume{}
	var stderr bytes.Buffer
	got := CaptureAndSaveResumeID(
		t.Context(),
		capturingAgent{err: errors.New("scan failed")},
		recorder,
		ResumeCapture{
			TaskDir: "/ws/A",
			Since:   time.Now(),
			Stderr:  &stderr,
		},
	)
	assert.Empty(t, got)
	assert.Empty(t, recorder.id)
	assert.Contains(t, stderr.String(), "J: scan failed")
}

func TestWatchAndSaveBackgroundResumeID_NotCapturer(t *testing.T) {
	recorder := &recordingResume{}
	got := WatchAndSaveBackgroundResumeID(
		t.Context(),
		stubAgent{},
		recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &bytes.Buffer{}},
		os.Getpid(),
	)
	assert.Empty(t, got)
	assert.Empty(t, recorder.id)
}

func TestWatchAndSaveBackgroundResumeID_InvalidPID(t *testing.T) {
	recorder := &recordingResume{}
	got := WatchAndSaveBackgroundResumeID(
		t.Context(),
		capturingAgent{id: "ignored"},
		recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &bytes.Buffer{}},
		0,
	)
	assert.Empty(t, got)
	assert.Empty(t, recorder.id)
}

func TestWatchAndSaveBackgroundResumeID_ImmediateCapture(t *testing.T) {
	recorder := &recordingResume{}
	got := WatchAndSaveBackgroundResumeID(
		t.Context(),
		capturingAgent{id: "active"},
		recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &bytes.Buffer{}},
		os.Getpid(),
	)
	assert.Equal(t, "active", got)
	assert.Equal(t, "active", recorder.id)
}

func TestWatchAndSaveBackgroundResumeID_DelayedCapture(t *testing.T) {
	recorder := &recordingResume{}
	dir := t.TempDir()
	agent := &delayedCapturingAgent{}
	done := make(chan string, 1)
	go func() {
		done <- WatchAndSaveBackgroundResumeID(
			t.Context(),
			agent,
			recorder,
			ResumeCapture{TaskDir: dir, Stderr: &bytes.Buffer{}},
			os.Getpid(),
		)
	}()
	require.Eventually(t, func() bool {
		return agent.calls.Load() >= 2
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "rollout.jsonl"), []byte("x"), 0o600,
	))
	select {
	case got := <-done:
		assert.Equal(t, "delayed", got)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for capture")
	}
	assert.Equal(t, "delayed", recorder.id)
}

func TestWatchAndSaveBackgroundResumeID_PIDExited(t *testing.T) {
	recorder := &recordingResume{}
	got := WatchAndSaveBackgroundResumeID(
		t.Context(),
		capturingAgent{},
		recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &bytes.Buffer{}},
		999999,
	)
	assert.Empty(t, got)
	assert.Empty(t, recorder.id)
}

func TestWatchAndSaveBackgroundResumeID_ContextCancelled(t *testing.T) {
	recorder := &recordingResume{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got := WatchAndSaveBackgroundResumeID(
		ctx,
		capturingAgent{},
		recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &bytes.Buffer{}},
		os.Getpid(),
	)
	assert.Empty(t, got)
}

func TestCaptureAndSaveProcessResumeID_ActiveCapture(t *testing.T) {
	recorder := &recordingResume{}
	got, err := CaptureAndSaveProcessResumeID(
		t.Context(),
		capturingAgent{id: "process"},
		recorder,
		ResumeCapture{
			TaskDir: t.TempDir(),
			Stderr:  &bytes.Buffer{},
		},
		ResumeProcess{PID: os.Getpid()},
	)
	require.NoError(t, err)
	assert.Equal(t, "process", got)
	assert.Equal(t, "process", recorder.id)
}

func TestCaptureAndSaveProcessResumeID_ReusesPriorID(t *testing.T) {
	recorder := &recordingResume{}
	got, err := CaptureAndSaveProcessResumeID(
		t.Context(),
		capturingAgent{id: "ignored"},
		recorder,
		ResumeCapture{Stderr: &bytes.Buffer{}},
		ResumeProcess{ResumeID: "prior"},
	)
	require.NoError(t, err)
	assert.Equal(t, "prior", got)
	assert.Empty(t, recorder.id)
}

func TestCaptureAndSaveProcessResumeID_WaitError(t *testing.T) {
	recorder := &recordingResume{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := CaptureAndSaveProcessResumeID(
		ctx,
		stubAgent{},
		recorder,
		ResumeCapture{Stderr: &bytes.Buffer{}},
		ResumeProcess{
			PID:  os.Getpid(),
			Wait: true,
		},
	)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, got)
}

func TestWaitForResumeProcess(t *testing.T) {
	require.NoError(t, WaitForResumeProcess(t.Context(), 0))
}

// TestStartAndSaveForegroundResumeID_ExistingID pins the no-op
// contract: a non-empty existingID short-circuits — no capturer
// methods run and the stop function returns "".
func TestStartAndSaveForegroundResumeID_ExistingID(t *testing.T) {
	recorder := &recordingResume{}
	agent := &delayedCapturingAgent{}
	stop := StartAndSaveForegroundResumeID(
		t.Context(), agent, recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &bytes.Buffer{}},
		"prior", true,
	)
	assert.Empty(t, stop())
	assert.Empty(t, recorder.id)
	assert.Equal(t, int32(0), agent.calls.Load())
}

// TestStartAndSaveForegroundResumeID_NotInteractive pins the
// headless no-op branch: a non-interactive run never starts a
// watcher (the pid-driven background watcher handles capture there),
// so no capturer methods run and the stop function returns "".
func TestStartAndSaveForegroundResumeID_NotInteractive(t *testing.T) {
	recorder := &recordingResume{}
	agent := &delayedCapturingAgent{}
	stop := StartAndSaveForegroundResumeID(
		t.Context(), agent, recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &bytes.Buffer{}},
		"", false,
	)
	assert.Empty(t, stop())
	assert.Empty(t, recorder.id)
	assert.Equal(t, int32(0), agent.calls.Load())
}

// TestStartAndSaveForegroundResumeID_NotCapturer pins the second
// no-op branch: cursor/claude-style backends without
// ResumeIDCapturer return a stop that yields "".
func TestStartAndSaveForegroundResumeID_NotCapturer(t *testing.T) {
	recorder := &recordingResume{}
	stop := StartAndSaveForegroundResumeID(
		t.Context(), stubAgent{}, recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &bytes.Buffer{}},
		"", true,
	)
	assert.Empty(t, stop())
	assert.Empty(t, recorder.id)
}

// TestStartAndSaveForegroundResumeID_ImmediateCapture pins the
// fast path where the session metadata already exists before the
// TUI starts — the recorder is stamped synchronously and stop
// returns the captured id without ever launching the goroutine.
func TestStartAndSaveForegroundResumeID_ImmediateCapture(t *testing.T) {
	recorder := &recordingResume{}
	stop := StartAndSaveForegroundResumeID(
		t.Context(),
		capturingAgent{id: "ready"},
		recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &bytes.Buffer{}},
		"", true,
	)
	assert.Equal(t, "ready", recorder.id)
	assert.Equal(t, "ready", stop())
}

// TestStartAndSaveForegroundResumeID_LiveCapture pins the
// requirements-driven scenario: metadata appears while the TUI is
// still running. The recorder must see the captured id before the
// caller invokes stop, and stop must return it.
func TestStartAndSaveForegroundResumeID_LiveCapture(t *testing.T) {
	recorder := &recordingResume{}
	dir := t.TempDir()
	agent := &delayedCapturingAgent{}
	stop := StartAndSaveForegroundResumeID(
		t.Context(), agent, recorder,
		ResumeCapture{TaskDir: dir, Stderr: &bytes.Buffer{}},
		"", true,
	)
	require.Eventually(t, func() bool {
		return agent.calls.Load() >= 2
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "rollout.jsonl"), []byte("x"), 0o600,
	))
	require.Eventually(t, func() bool {
		return recorder.snapshot() == "delayed"
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "delayed", stop())
}

// TestStartAndSaveForegroundResumeID_StopWithoutCapture pins the
// timeout-equivalent path: when no metadata appears, stop cancels
// the goroutine and returns "" without hanging the caller.
func TestStartAndSaveForegroundResumeID_StopWithoutCapture(t *testing.T) {
	recorder := &recordingResume{}
	agent := &delayedCapturingAgent{}
	stop := StartAndSaveForegroundResumeID(
		t.Context(), agent, recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &bytes.Buffer{}},
		"", true,
	)
	done := make(chan string, 1)
	go func() { done <- stop() }()
	select {
	case got := <-done:
		assert.Empty(t, got)
	case <-time.After(2 * time.Second):
		t.Fatal("stop() blocked when no metadata was written")
	}
	assert.Empty(t, recorder.id)
}

// TestStartAndSaveForegroundResumeID_ImmediateCaptureError pins
// the warning-and-continue contract: an immediate scan error is
// written to stderr and the goroutine still runs so a later
// successful scan can record the id.
func TestStartAndSaveForegroundResumeID_ImmediateCaptureError(
	t *testing.T,
) {
	recorder := &recordingResume{}
	var stderr bytes.Buffer
	stop := StartAndSaveForegroundResumeID(
		t.Context(),
		capturingAgent{err: errors.New("scan blew up")},
		recorder,
		ResumeCapture{TaskDir: t.TempDir(), Stderr: &stderr},
		"", true,
	)
	assert.Empty(t, stop())
	assert.Contains(t, stderr.String(), "J: scan blew up")
}
