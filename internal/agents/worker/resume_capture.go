package worker

import (
	"context"
	"fmt"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/util/run"
)

const (
	activeResumeCaptureTimeout = 2 * time.Second
	activeResumeCapturePoll    = 100 * time.Millisecond
)

func captureSpawnedWorkerResume(
	ctx context.Context,
	agent codingagents.Agent,
	lc *lifecycle.WorkLifecycle,
	capture codingagents.ResumeCapture,
	resumeID string,
	pid int,
) string {
	if resumeID != "" {
		return resumeID
	}
	return captureWorkerResumeWhileActive(ctx, agent, lc, capture, pid)
}

func captureWorkerResumeWhileActive(
	ctx context.Context,
	agent codingagents.Agent,
	lc *lifecycle.WorkLifecycle,
	capture codingagents.ResumeCapture,
	pid int,
) string {
	id, done := captureWorkerResumeOnce(ctx, agent, lc, capture)
	if done || !run.IsAlive(pid) {
		return id
	}

	timeout := time.NewTimer(activeResumeCaptureTimeout)
	defer timeout.Stop()
	ticker := time.NewTicker(activeResumeCapturePoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ""
		case <-timeout.C:
			return ""
		case <-ticker.C:
			id, done := captureWorkerResumeOnce(ctx, agent, lc, capture)
			if done || !run.IsAlive(pid) {
				return id
			}
		}
	}
}

func captureWorkerResumeOnce(
	ctx context.Context,
	agent codingagents.Agent,
	lc *lifecycle.WorkLifecycle,
	capture codingagents.ResumeCapture,
) (string, bool) {
	id, err := codingagents.CaptureResumeID(
		ctx, agent, capture.TaskDir, capture.Since,
	)
	if err != nil {
		fmt.Fprintf(capture.Stderr, "J: %v\n", err)
		return "", true
	}
	if id == "" {
		return "", false
	}
	lc.RecordResumeSession(id)
	return id, true
}
