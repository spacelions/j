// Package agentlog emits single-line structured marker events
// interleaved with the human transcript inside a per-task `agent.log`.
//
// Every marker is a JSON object on its own line, prefixed with a fixed
// sentinel:
//
//	>>> J {"event":"phase_begin","ts":"2026-05-04T12:00:00Z","phase":"plan","task":"01KQ…"}
//
// The sentinel is unique to this codebase, so a downstream consumer
// can split the file into the structured stream (lines that start
// with the sentinel) and the human transcript (lines that don't) with
// `rg '^>>> J '`.
//
// Markers are best-effort: emit failures (closed file, full disk) are
// returned to the caller but the lifecycle / orchestrator code that
// drives Emit is expected to swallow them, because losing a marker is
// strictly less harmful than aborting a phase.
package agentlog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Sentinel prefixes every marker line. Using a string with characters
// that are extremely unlikely to occur naturally at line-start in
// agent stdout/stderr keeps the grep-out trivial without escaping.
const Sentinel = ">>> J "

// emitMu serialises Emit calls within a single process. Cross-process
// atomicity is provided by O_APPEND on POSIX (small writes append
// atomically on the kernels j targets); the in-process mutex is the
// belt to that suspenders so a goroutine + lifecycle interleave can't
// chunk a single marker line in half.
var emitMu sync.Mutex

// Emit writes one marker line to w. fields are merged into the JSON
// payload alongside the auto-populated `event` and `ts` keys; `ts`
// uses RFC3339 with nanosecond precision in UTC so log lines from
// different time zones still sort chronologically.
//
// A nil w is rejected — callers that have no writer (e.g. interactive
// runs without a per-task log) should skip the call entirely. An
// `event` collision in fields (caller passes "event" or "ts" keys) is
// silently overridden by the auto-populated values; the helper owns
// those slots.
func Emit(w io.Writer, event string, fields map[string]any) error {
	if w == nil {
		return nil
	}
	payload := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		payload[k] = v
	}
	payload["event"] = event
	payload["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("agentlog: marshal %s: %w", event, err)
	}
	emitMu.Lock()
	defer emitMu.Unlock()
	if _, err := fmt.Fprintf(w, "%s%s\n", Sentinel, data); err != nil {
		return fmt.Errorf("agentlog: write %s: %w", event, err)
	}
	return nil
}

// EmitTo opens path in O_APPEND mode and emits one marker. An empty
// path is a silent no-op: the foreground (interactive) flows do not
// share a per-task log, so callers that don't know whether they are
// foreground or detached can hand the empty string and not branch.
//
// The file handle is closed before EmitTo returns so a long-lived
// caller (the SpawnIn reap goroutine) does not pin the inode after
// the parent finalises the task.
func EmitTo(path string, event string, fields map[string]any) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("agentlog: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return Emit(f, event, fields)
}
