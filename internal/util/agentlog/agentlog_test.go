package agentlog

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// markerHeader matches the human-readable marker prefix:
// `<RFC3339Z>  <topic> <verb>` (verb optional).
var markerHeader = regexp.MustCompile(
	`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z  \S+( \S+)?`,
)

// TestEmit_LineShape pins the on-the-wire format: every marker is one
// line that starts with an RFC3339Z timestamp + two spaces, contains
// the topic+verb derived from the event name, and renders fields as
// sorted `k=v` pairs after an em dash.
func TestEmit_LineShape(t *testing.T) {
	var buf bytes.Buffer
	err := Emit(&buf, "session_start", map[string]any{
		"task": "01KQ", "phase": "full",
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	line := buf.String()
	if !strings.HasSuffix(line, "\n") {
		t.Fatalf("missing trailing newline: %q", line)
	}
	trimmed := strings.TrimSuffix(line, "\n")
	if !markerHeader.MatchString(trimmed) {
		t.Fatalf("missing marker header prefix: %q", trimmed)
	}
	if !strings.Contains(trimmed, "session start") {
		t.Fatalf("missing topic+verb: %q", trimmed)
	}
	// keys sorted lexicographically: phase before task.
	if !strings.Contains(trimmed, "— phase=full task=01KQ") {
		t.Fatalf("sorted fields missing: %q", trimmed)
	}
	tsStr := trimmed[:len("2026-05-04T12:00:00Z")]
	if _, err := time.Parse(time.RFC3339, tsStr); err != nil {
		t.Fatalf("ts not RFC3339: %v (%q)", err, tsStr)
	}
}

// TestEmit_NoUnderscoreEvent pins the verb-empty branch: an event
// name without an underscore renders as just the topic.
func TestEmit_NoUnderscoreEvent(t *testing.T) {
	var buf bytes.Buffer
	if err := Emit(&buf, "ready", nil); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "  ready\n") {
		t.Fatalf("expected bare topic, got %q", got)
	}
	if strings.Contains(got, "—") {
		t.Fatalf("expected no field separator, got %q", got)
	}
}

// TestEmit_FieldsSortedAndSkipEmpty asserts fields render in
// lexicographic key order and empty values are dropped.
func TestEmit_FieldsSortedAndSkipEmpty(t *testing.T) {
	var buf bytes.Buffer
	err := Emit(&buf, "child_exit", map[string]any{
		"name":  "claude",
		"pid":   1234,
		"empty": "",
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	idxName := strings.Index(got, "name=claude")
	idxPid := strings.Index(got, "pid=1234")
	if idxName < 0 || idxPid < 0 {
		t.Fatalf("missing fields: %q", got)
	}
	if idxName > idxPid {
		t.Fatalf("expected name= before pid=: %q", got)
	}
	if strings.Contains(got, "empty=") {
		t.Fatalf("empty value not skipped: %q", got)
	}
	if !strings.Contains(got, "child exit") {
		t.Fatalf("missing topic+verb: %q", got)
	}
}

// TestEmit_ReservedKeysOverridden confirms that caller-supplied
// `event` / `ts` keys are silently skipped; the formatter owns the
// header and a caller cannot mislabel a marker.
func TestEmit_ReservedKeysOverridden(t *testing.T) {
	var buf bytes.Buffer
	err := Emit(&buf, "session_start", map[string]any{
		"event": "wrong",
		"ts":    "wrong",
		"task":  "01KQ",
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := buf.String()
	if strings.Contains(got, "wrong") {
		t.Fatalf("reserved keys leaked: %q", got)
	}
	if !strings.Contains(got, "session start") {
		t.Fatalf("formatter header missing: %q", got)
	}
	if !strings.Contains(got, "task=01KQ") {
		t.Fatalf("real field missing: %q", got)
	}
}

// TestEmit_NilWriterIsNoop pins the nil-writer branch: callers that
// have no per-task log (interactive flows) can pass nil without
// branching themselves.
func TestEmit_NilWriterIsNoop(t *testing.T) {
	if err := Emit(nil, "session_start", nil); err != nil {
		t.Fatalf("Emit(nil): %v", err)
	}
}

// TestEmit_Concurrent confirms intra-process serialisation: 50
// concurrent Emit calls produce 50 well-formed lines without
// interleaving (each line still matches the marker header pattern).
func TestEmit_Concurrent(t *testing.T) {
	var buf bytes.Buffer
	var wg sync.WaitGroup
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := Emit(&buf, "session_start", map[string]any{
				"i": i,
			})
			if err != nil {
				t.Errorf("Emit: %v", err)
			}
		}(i)
	}
	wg.Wait()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("got %d lines, want %d", len(lines), n)
	}
	for _, line := range lines {
		if !markerHeader.MatchString(line) {
			t.Fatalf("torn line: %q", line)
		}
	}
}

// TestEmitTo_AppendsAcrossCalls pins the append-only invariant on the
// shared agent.log: a second EmitTo against the same path must
// preserve every byte from the first.
func TestEmitTo_AppendsAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	if err := EmitTo(path, "plan_begin", nil); err != nil {
		t.Fatalf("first EmitTo: %v", err)
	}
	if err := EmitTo(path, "plan_done", nil); err != nil {
		t.Fatalf("second EmitTo: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "plan begin") {
		t.Fatalf("first marker missing: %q", got)
	}
	if !strings.Contains(got, "plan done") {
		t.Fatalf("second marker missing: %q", got)
	}
	if strings.Index(got, "plan begin") > strings.Index(got, "plan done") {
		t.Fatalf("expected chronological order: %q", got)
	}
}

// TestEmitTo_PreservesPriorBytes confirms EmitTo opens the log in
// O_APPEND mode rather than truncating: arbitrary human transcript
// written before the marker survives the open.
func TestEmitTo_PreservesPriorBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	err := os.WriteFile(path, []byte("human transcript line\n"), 0o644)
	if err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := EmitTo(path, "plan_begin", nil); err != nil {
		t.Fatalf("EmitTo: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got := string(data)
	if !strings.HasPrefix(got, "human transcript line\n") {
		t.Fatalf("prior bytes lost: %q", got)
	}
	if !strings.Contains(got, "plan begin") {
		t.Fatalf("marker missing: %q", got)
	}
}

// TestEmitTo_EmptyPath pins the silent no-op branch: callers that
// don't know whether they are foreground or detached can pass "".
func TestEmitTo_EmptyPath(t *testing.T) {
	if err := EmitTo("", "plan_begin", nil); err != nil {
		t.Fatalf("EmitTo(\"\"): %v", err)
	}
}

// TestEmitTo_OpenFailureIsWrapped covers the path-is-a-directory
// branch so the wrapped error message includes the offending path.
func TestEmitTo_OpenFailureIsWrapped(t *testing.T) {
	dir := t.TempDir()
	err := EmitTo(dir, "plan_begin", nil)
	if err == nil {
		t.Fatal("expected open error when path is a directory")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("err = %v, want path in message", err)
	}
}

// TestHeader covers Header's three branches: no-underscore, single
// underscore, and trailing-underscore (verb empty).
func TestHeader(t *testing.T) {
	cases := []struct{ in, want string }{
		{"session_start", "session start"},
		{"child_exit", "child exit"},
		{"ready", "ready"},
		{"plan_", "plan"},
	}
	for _, c := range cases {
		if got := Header(c.in); got != c.want {
			t.Errorf("Header(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestEmit_DurationField pins the time.Duration branch in
// formatValue: durations render as their int64 nanosecond count so
// callers that already pre-convert to milliseconds get a clean
// integer string.
func TestEmit_DurationField(t *testing.T) {
	var buf bytes.Buffer
	err := Emit(&buf, "child_exit", map[string]any{
		"d": 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(buf.String(), "d=250000000") {
		t.Fatalf("duration not int64 ns: %q", buf.String())
	}
}

// failingWriter always returns an error from Write so the wrapped
// write-error branch is exercised.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errBoom }

var errBoom = boomError("boom")

type boomError string

func (b boomError) Error() string { return string(b) }

// TestEmit_WriteErrorIsWrapped covers the io.Writer Write error
// branch so the surfaced error mentions the event for triage.
func TestEmit_WriteErrorIsWrapped(t *testing.T) {
	err := Emit(failingWriter{}, "session_start", nil)
	if err == nil {
		t.Fatal("expected write error")
	}
	if !strings.Contains(err.Error(), "session_start") {
		t.Fatalf("err = %v, want event in message", err)
	}
}
