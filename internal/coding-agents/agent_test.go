package codingagents

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
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

type delayedCapturingAgent struct {
	stubAgent
	calls int
}

func (d *delayedCapturingAgent) CaptureResumeID(
	_ context.Context, _ string, _ time.Time,
) (string, error) {
	d.calls++
	if d.calls == 1 {
		return "", nil
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
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// TestCaptureResumeID_Implemented pins the happy path: the
// implementer's return values are passed through verbatim.
func TestCaptureResumeID_Implemented(t *testing.T) {
	a := capturingAgent{id: "captured", err: nil}
	got, err := CaptureResumeID(
		t.Context(), a, "/ws/A", time.Now(),
	)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != "captured" {
		t.Fatalf("got %q, want captured", got)
	}
}

// TestCaptureResumeID_ImplementedError pins the error pass-through.
func TestCaptureResumeID_ImplementedError(t *testing.T) {
	want := errors.New("scan blew up")
	a := capturingAgent{id: "", err: want}
	_, err := CaptureResumeID(
		t.Context(), a, "/ws/A", time.Now(),
	)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

type recordingResume struct {
	id string
}

func (r *recordingResume) RecordResumeSession(id string) {
	r.id = id
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
	if got != "captured" {
		t.Fatalf("CaptureAndSaveResumeID = %q, want captured", got)
	}
	if recorder.id != "captured" {
		t.Fatalf("recorded id = %q, want captured", recorder.id)
	}
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
	if got != "" {
		t.Fatalf("CaptureAndSaveResumeID = %q, want empty", got)
	}
	if recorder.id != "" {
		t.Fatalf("recorded id = %q, want empty", recorder.id)
	}
	if !strings.Contains(stderr.String(), "J: scan failed") {
		t.Fatalf("stderr = %q, want scan warning", stderr.String())
	}
}

func TestCaptureAndSaveActiveResumeID_NotCapturer(t *testing.T) {
	recorder := &recordingResume{}
	got := CaptureAndSaveActiveResumeID(
		t.Context(),
		stubAgent{},
		recorder,
		ResumeCapture{Stderr: &bytes.Buffer{}},
		os.Getpid(),
	)
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestCaptureAndSaveActiveResumeID_RecordsImmediateID(t *testing.T) {
	recorder := &recordingResume{}
	got := CaptureAndSaveActiveResumeID(
		t.Context(),
		capturingAgent{id: "active"},
		recorder,
		ResumeCapture{Stderr: &bytes.Buffer{}},
		os.Getpid(),
	)
	if got != "active" {
		t.Fatalf("got %q, want active", got)
	}
	if recorder.id != "active" {
		t.Fatalf("recorded id = %q, want active", recorder.id)
	}
}

func TestCaptureAndSaveActiveResumeID_RecordsDelayedID(t *testing.T) {
	recorder := &recordingResume{}
	agent := &delayedCapturingAgent{}
	got := CaptureAndSaveActiveResumeID(
		t.Context(),
		agent,
		recorder,
		ResumeCapture{Stderr: &bytes.Buffer{}},
		os.Getpid(),
	)
	if got != "delayed" {
		t.Fatalf("got %q, want delayed", got)
	}
	if recorder.id != "delayed" {
		t.Fatalf("recorded id = %q, want delayed", recorder.id)
	}
}

func TestCaptureAndSaveActiveResumeID_WarnsOnCaptureError(t *testing.T) {
	recorder := &recordingResume{}
	var stderr bytes.Buffer
	got := CaptureAndSaveActiveResumeID(
		t.Context(),
		capturingAgent{err: errors.New("scan failed")},
		recorder,
		ResumeCapture{Stderr: &stderr},
		os.Getpid(),
	)
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if !strings.Contains(stderr.String(), "J: scan failed") {
		t.Fatalf("stderr = %q, want scan warning", stderr.String())
	}
}

func TestCaptureAndSaveActiveResumeID_ContextCanceled(t *testing.T) {
	recorder := &recordingResume{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got := CaptureAndSaveActiveResumeID(
		ctx,
		capturingAgent{},
		recorder,
		ResumeCapture{Stderr: &bytes.Buffer{}},
		os.Getpid(),
	)
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestCaptureAndSaveActiveResumeID_ProcessExited(t *testing.T) {
	recorder := &recordingResume{}
	got := CaptureAndSaveActiveResumeID(
		t.Context(),
		capturingAgent{},
		recorder,
		ResumeCapture{Stderr: &bytes.Buffer{}},
		999999,
	)
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestCaptureAndSaveActiveResumeID_TimesOut(t *testing.T) {
	recorder := &recordingResume{}
	got := CaptureAndSaveActiveResumeID(
		t.Context(),
		capturingAgent{},
		recorder,
		ResumeCapture{Stderr: &bytes.Buffer{}},
		os.Getpid(),
	)
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestCaptureAndSaveProcessResumeID_ActiveCaptureAndWait(t *testing.T) {
	recorder := &recordingResume{}
	got, err := CaptureAndSaveProcessResumeID(
		t.Context(),
		capturingAgent{id: "process"},
		recorder,
		ResumeCapture{Stderr: &bytes.Buffer{}},
		ResumeProcess{
			PID:  os.Getpid(),
			Wait: false,
		},
	)
	if err != nil {
		t.Fatalf("CaptureAndSaveProcessResumeID: %v", err)
	}
	if got != "process" {
		t.Fatalf("got %q, want process", got)
	}
	if recorder.id != "process" {
		t.Fatalf("recorded id = %q, want process", recorder.id)
	}
}

func TestCaptureAndSaveProcessResumeID_PostWaitFallback(t *testing.T) {
	recorder := &recordingResume{}
	got, err := CaptureAndSaveProcessResumeID(
		t.Context(),
		capturingAgent{id: "fallback"},
		recorder,
		ResumeCapture{Stderr: &bytes.Buffer{}},
		ResumeProcess{Wait: true},
	)
	if err != nil {
		t.Fatalf("CaptureAndSaveProcessResumeID: %v", err)
	}
	if got != "fallback" {
		t.Fatalf("got %q, want fallback", got)
	}
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
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestWaitForResumeProcess(t *testing.T) {
	if err := WaitForResumeProcess(t.Context(), 0); err != nil {
		t.Fatalf("WaitForResumeProcess: %v", err)
	}
}
