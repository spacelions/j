// Package agentlog emits single-line, human-readable structured marker
// events interleaved with the human transcript inside a per-task
// `agent.log`.
//
// Every marker is one line of the form
//
//	2026-05-04T12:00:00Z  session start — task=01KQ phase=full
//
// The leading RFC3339Z timestamp + two spaces matches the lifecycle
// status hook's own marker format, so a tailer can read the file as
// continuous human text without filtering.
//
// Markers are best-effort: emit failures (closed file, full disk) are
// returned to the caller but the lifecycle / orchestrator code that
// drives Emit is expected to swallow them, because losing a marker is
// strictly less harmful than aborting a phase.
package agentlog

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// emitMu serialises Emit calls within a single process. Cross-process
// atomicity is provided by O_APPEND on POSIX (small writes append
// atomically on the kernels j targets); the in-process mutex is the
// belt to that suspenders so a goroutine + lifecycle interleave can't
// chunk a single marker line in half.
var emitMu sync.Mutex

// Header returns the topic+verb prefix for the given event name. The
// event is split on the first underscore: `session_start` becomes
// `session start`, `child_exit` becomes `child exit`. An event with
// no underscore returns just the event itself (verb is empty).
//
// Exported so tests that guard against marker leakage to stderr can
// build the same prefix the writer uses without re-deriving it.
func Header(event string) string {
	topic, verb, ok := strings.Cut(event, "_")
	if !ok || verb == "" {
		return topic
	}
	return topic + " " + verb
}

// Emit writes one marker line to w: an RFC3339Z timestamp, two spaces,
// the topic+verb derived from event, and — when fields is non-empty —
// `— k=v k=v` with keys sorted lexicographically. Empty values are
// skipped; integers are formatted as decimal, time.Duration as the
// underlying int64 nanoseconds, and everything else via %v.
//
// A nil w is a silent no-op so callers without a per-task log
// (interactive flows) can hand nil and not branch.
func Emit(w io.Writer, event string, fields map[string]any) error {
	if w == nil {
		return nil
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	line := ts + "  " + Header(event)
	if pairs := formatFields(fields); pairs != "" {
		line += " — " + pairs
	}
	emitMu.Lock()
	defer emitMu.Unlock()
	if _, err := fmt.Fprintln(w, line); err != nil {
		return fmt.Errorf("agentlog: write %s: %w", event, err)
	}
	return nil
}

// formatFields renders fields as a `k=v k=v` string with keys sorted
// lexicographically. Empty/zero string values and the reserved `event`
// / `ts` keys are skipped — the formatter owns those slots and the
// caller cannot override them.
func formatFields(fields map[string]any) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k == "event" || k == "ts" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		v := formatValue(fields[k])
		if v == "" {
			continue
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, " ")
}

// formatValue renders one field value. nil and empty strings render as
// "" so formatFields can drop them; integers print as decimal,
// time.Duration as its int64 nanoseconds (callers already pass
// already-converted milliseconds for human readability).
func formatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case time.Duration:
		return strconv.FormatInt(int64(x), 10)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// WriteLines appends one or more pre-formatted lines to w under the
// same emit lock Emit uses, so concurrent SpawnPipedIn stdout / stderr
// readers and the lifecycle marker writer cannot interleave mid-line.
// Each line gets a trailing `\n` if it lacks one. A nil w or zero-len
// batch is a silent no-op (mirrors Emit's nil-writer convention).
func WriteLines(w io.Writer, lines [][]byte) error {
	if w == nil || len(lines) == 0 {
		return nil
	}
	emitMu.Lock()
	defer emitMu.Unlock()
	for _, l := range lines {
		if _, err := w.Write(l); err != nil {
			return fmt.Errorf("agentlog: write line: %w", err)
		}
		if bytes.HasSuffix(l, []byte{'\n'}) {
			continue
		}
		if _, err := w.Write([]byte{'\n'}); err != nil {
			return fmt.Errorf(
				"agentlog: write newline: %w", err)
		}
	}
	return nil
}

// EmitTo opens path in O_APPEND mode and emits one marker. An empty
// path is a silent no-op: the foreground (interactive) flows do not
// share a per-task log, so callers that don't know whether they are
// foreground or detached can hand the empty string and not branch.
func EmitTo(path, event string, fields map[string]any) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(
		path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("agentlog: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return Emit(f, event, fields)
}
