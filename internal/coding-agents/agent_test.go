package codingagents

import (
	"context"
	"errors"
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
