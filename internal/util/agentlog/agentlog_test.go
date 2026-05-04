package agentlog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestEmit_MarkerShape pins the on-the-wire format: every marker is
// one line that starts with the sentinel, parses as JSON, and carries
// the event + ts + caller-supplied fields.
func TestEmit_MarkerShape(t *testing.T) {
	var buf bytes.Buffer
	if err := Emit(&buf, "phase_begin", map[string]any{"phase": "plan", "task": "01KQ"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	line := buf.String()
	if !strings.HasPrefix(line, Sentinel) {
		t.Fatalf("missing sentinel: %q", line)
	}
	if !strings.HasSuffix(line, "\n") {
		t.Fatalf("missing trailing newline: %q", line)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(line, Sentinel), "\n")
	var got map[string]any
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("payload not JSON: %v (%q)", err, payload)
	}
	if got["event"] != "phase_begin" {
		t.Fatalf("event = %v", got["event"])
	}
	if got["phase"] != "plan" {
		t.Fatalf("phase = %v", got["phase"])
	}
	if got["task"] != "01KQ" {
		t.Fatalf("task = %v", got["task"])
	}
	tsStr, ok := got["ts"].(string)
	if !ok {
		t.Fatalf("ts missing or wrong type: %v", got["ts"])
	}
	if _, err := time.Parse(time.RFC3339Nano, tsStr); err != nil {
		t.Fatalf("ts not RFC3339Nano: %v (%q)", err, tsStr)
	}
}

// TestEmit_EventOverridesField confirms the auto-populated `event`
// key wins over a caller-supplied collision; the helper owns that
// slot so callers cannot accidentally mislabel a marker.
func TestEmit_EventOverridesField(t *testing.T) {
	var buf bytes.Buffer
	if err := Emit(&buf, "phase_end", map[string]any{"event": "wrong", "ts": "wrong"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var got map[string]any
	payload := strings.TrimSuffix(strings.TrimPrefix(buf.String(), Sentinel), "\n")
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if got["event"] != "phase_end" {
		t.Fatalf("event = %v, want phase_end", got["event"])
	}
	if got["ts"] == "wrong" {
		t.Fatalf("ts not overridden: %v", got["ts"])
	}
}

// TestEmit_NilWriterIsNoop pins the nil-writer branch: callers that
// have no per-task log (interactive flows) can pass nil without
// branching themselves.
func TestEmit_NilWriterIsNoop(t *testing.T) {
	if err := Emit(nil, "phase_begin", nil); err != nil {
		t.Fatalf("Emit(nil): %v", err)
	}
}

// TestEmit_Concurrent confirms intra-process serialisation: 50
// concurrent Emit calls produce 50 well-formed lines without
// interleaving (each line still parses as JSON on its own).
func TestEmit_Concurrent(t *testing.T) {
	var buf bytes.Buffer
	var wg sync.WaitGroup
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := Emit(&buf, "phase_begin", map[string]any{"i": i}); err != nil {
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
		if !strings.HasPrefix(line, Sentinel) {
			t.Fatalf("torn line: %q", line)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, Sentinel)), &got); err != nil {
			t.Fatalf("torn JSON: %v (%q)", err, line)
		}
	}
}

// TestEmitTo_AppendsAcrossCalls pins the append-only invariant on the
// shared agent.log: a second EmitTo against the same path must
// preserve every byte from the first.
func TestEmitTo_AppendsAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	if err := EmitTo(path, "phase_begin", map[string]any{"phase": "plan"}); err != nil {
		t.Fatalf("first EmitTo: %v", err)
	}
	if err := EmitTo(path, "phase_end", map[string]any{"phase": "plan"}); err != nil {
		t.Fatalf("second EmitTo: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"event":"phase_begin"`) {
		t.Fatalf("first marker missing: %q", got)
	}
	if !strings.Contains(got, `"event":"phase_end"`) {
		t.Fatalf("second marker missing: %q", got)
	}
	if strings.Index(got, "phase_begin") > strings.Index(got, "phase_end") {
		t.Fatalf("expected chronological order: %q", got)
	}
}

// TestEmitTo_PreservesPriorBytes confirms EmitTo opens the log in
// O_APPEND mode rather than truncating: arbitrary human transcript
// written before the marker survives the open.
func TestEmitTo_PreservesPriorBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	if err := os.WriteFile(path, []byte("human transcript line\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := EmitTo(path, "phase_begin", map[string]any{"phase": "plan"}); err != nil {
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
	if !strings.Contains(got, Sentinel) {
		t.Fatalf("marker missing: %q", got)
	}
}

// TestEmitTo_EmptyPath pins the silent no-op branch: callers that
// don't know whether they are foreground or detached can pass "".
func TestEmitTo_EmptyPath(t *testing.T) {
	if err := EmitTo("", "phase_begin", nil); err != nil {
		t.Fatalf("EmitTo(\"\"): %v", err)
	}
}

// TestEmitTo_OpenFailureIsWrapped covers the path-is-a-directory
// branch so the wrapped error message includes the offending path.
func TestEmitTo_OpenFailureIsWrapped(t *testing.T) {
	dir := t.TempDir()
	err := EmitTo(dir, "phase_begin", nil)
	if err == nil {
		t.Fatal("expected open error when path is a directory")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("err = %v, want path in message", err)
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
	err := Emit(failingWriter{}, "phase_begin", nil)
	if err == nil {
		t.Fatal("expected write error")
	}
	if !strings.Contains(err.Error(), "phase_begin") {
		t.Fatalf("err = %v, want event in message", err)
	}
}
