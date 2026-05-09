package claude

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// rfc3339Z matches an `RFC3339Z + two spaces` marker prefix so the
// goldens can be timestamp-stable: each rendered line gets its
// timestamp swapped for the literal `TS` before comparison.
var rfc3339Z = regexp.MustCompile(
	`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z  `,
)

// TestFormatLog_GoldenFiles drives the formatter over the captured
// stream-json fixtures under testdata/format/*.jsonl and compares the
// rendered output against paired *.golden files. Set
// `UPDATE_GOLDEN=1` to regenerate the goldens after a deliberate
// formatter change. The timestamp prefix on every marker line is
// swapped for `TS  ` before comparison so the goldens stay stable.
func TestFormatLog_GoldenFiles(t *testing.T) {
	t.Parallel()
	cases, err := filepath.Glob("testdata/format/*.jsonl")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("no fixtures under testdata/format/*.jsonl")
	}
	for _, in := range cases {
		t.Run(filepath.Base(in), func(t *testing.T) {
			t.Parallel()
			runGoldenCase(t, in)
		})
	}
}

// runGoldenCase reads one fixture, applies FormatLog line-by-line,
// normalises timestamps, and compares to the paired *.golden. When
// UPDATE_GOLDEN=1 it (re)writes the golden file so a deliberate
// formatter change can be promoted in one go.
func runGoldenCase(t *testing.T, jsonlPath string) {
	t.Helper()
	a := New()
	got := normaliseTimestamps(applyFormatLog(t, a, jsonlPath))
	goldenPath := strings.TrimSuffix(jsonlPath, ".jsonl") + ".golden"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(
			goldenPath, []byte(got), 0o644,
		); err != nil {
			t.Fatalf("write golden %s: %v", goldenPath, err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Fatalf("formatter output diverged from golden\n"+
			"--- got ---\n%s--- want ---\n%s",
			got, string(want))
	}
}

// applyFormatLog runs the agent's FormatLog on every line of the
// fixture and returns the concatenated output. Trailing newlines on
// fixture lines are preserved so the formatter sees the on-the-wire
// shape (one event per `\n`-terminated line).
func applyFormatLog(t *testing.T, a *Agent, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	br := bufio.NewReader(f)
	var out bytes.Buffer
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			out.Write(a.FormatLog(line))
		}
		if err != nil {
			return out.String()
		}
	}
}

// normaliseTimestamps swaps every `<RFC3339Z>  ` prefix for `TS  ` so
// goldens stay stable across runs. Lines that do not start with a
// marker prefix (raw fall-through bytes) pass through unchanged.
func normaliseTimestamps(s string) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		out.WriteString(rfc3339Z.ReplaceAllString(line, "TS  "))
	}
	return out.String()
}

// TestFormatLog_MalformedJSON pins the parse-failure fall-through:
// non-JSON input (e.g. a child panic line) survives in agent.log
// verbatim.
func TestFormatLog_MalformedJSON(t *testing.T) {
	t.Parallel()
	in := []byte("panic: runtime error: bogus\n")
	got := New().FormatLog(in)
	if string(got) != string(in) {
		t.Fatalf("FormatLog = %q, want passthrough %q", got, in)
	}
}

// TestFormatLog_UnknownType pins the unknown-envelope fall-through:
// a parseable JSON line whose `type` is not one we render falls
// through to the raw line so future event types stay visible.
func TestFormatLog_UnknownType(t *testing.T) {
	t.Parallel()
	in := []byte(`{"type":"rate_limit_event","status":"rejected"}` +
		"\n")
	got := New().FormatLog(in)
	if string(got) != string(in) {
		t.Fatalf("FormatLog = %q, want passthrough %q", got, in)
	}
}

// TestFormatLog_EmptyLine pins the empty-input branch: a bare
// newline (or empty bytes) returns the input unchanged.
func TestFormatLog_EmptyLine(t *testing.T) {
	t.Parallel()
	for _, in := range [][]byte{nil, []byte("\n"), {}} {
		got := New().FormatLog(in)
		if string(got) != string(in) {
			t.Fatalf("FormatLog(%q) = %q, want passthrough", in, got)
		}
	}
}

// TestFormatLog_TextTruncation pins the rune-cap behaviour: a text
// block over the 200-rune limit emits one `…` suffix and a
// `chars=<n>` field carrying the original rune count.
func TestFormatLog_TextTruncation(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 250)
	src := []byte(`{"type":"assistant","message":{"content":` +
		`[{"type":"text","text":"` + long + `"}]}}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "chars=250") {
		t.Fatalf("missing chars=250: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("missing ellipsis: %q", got)
	}
	if strings.Contains(got, strings.Repeat("x", 250)) {
		t.Fatalf("untruncated body leaked: %q", got)
	}
}

// TestFormatLog_ToolResultIsError pins the tool_result error branch:
// `is_error: true` flips ok=false in the rendered marker.
func TestFormatLog_ToolResultIsError(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"user","message":{"content":` +
		`[{"type":"tool_result","tool_use_id":"<r>",` +
		`"content":"oops","is_error":true}]}}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "ok=false") {
		t.Fatalf("missing ok=false: %q", got)
	}
	if !strings.Contains(got, "bytes=4") {
		t.Fatalf("missing bytes=4: %q", got)
	}
}

// TestFormatLog_AssistantEmptyContent pins the empty-content
// fall-through for assistant messages: a content array of [] yields
// the raw line unchanged so a malformed envelope stays visible.
func TestFormatLog_AssistantEmptyContent(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"assistant","message":{"content":[]}}` +
		"\n")
	got := New().FormatLog(src)
	if string(got) != string(src) {
		t.Fatalf("FormatLog = %q, want raw %q", got, src)
	}
}

// TestFormatLog_UserEmptyContent pins the empty-content fall-through
// for user messages: a content array of [] yields the raw line.
func TestFormatLog_UserEmptyContent(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"user","message":{"content":[]}}` + "\n")
	got := New().FormatLog(src)
	if string(got) != string(src) {
		t.Fatalf("FormatLog = %q, want raw %q", got, src)
	}
}

// TestFormatLog_UserNonToolResult pins the no-tool_result branch:
// claude wraps tool results inside a user envelope; rare non-tool
// user content (e.g. a future text block inside `user`) falls
// through to the raw line.
func TestFormatLog_UserNonToolResult(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"user","message":{"content":` +
		`[{"type":"text","text":"unexpected"}]}}` + "\n")
	got := New().FormatLog(src)
	if string(got) != string(src) {
		t.Fatalf("FormatLog = %q, want raw %q", got, src)
	}
}

// TestFormatLog_UnknownBlockType pins the default block-renderer
// branch: an assistant content block with an unfamiliar `type`
// renders as a sparse `agent message` marker so agent.log stays
// readable.
func TestFormatLog_UnknownBlockType(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"assistant","message":{"content":` +
		`[{"type":"image","data":"base64…"}]}}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "agent message") {
		t.Fatalf("missing fallback marker: %q", got)
	}
}

// TestFormatLog_ToolResultBlocks pins the array-payload branch of
// toolResultBody: claude can emit tool_result.content as either a
// JSON string (most common) or an array of typed blocks. The
// formatter sums the text bytes either way.
func TestFormatLog_ToolResultBlocks(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"user","message":{"content":` +
		`[{"type":"tool_result","content":` +
		`[{"type":"text","text":"hello"}]}]}}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "bytes=5") {
		t.Fatalf("missing bytes=5 from array payload: %q", got)
	}
}

// TestFormatLog_ToolResultRawFallback pins the final json-decoder
// branch of toolResultBody: a payload that is neither a string nor
// an array (e.g. a number) falls back to the raw bytes for
// length-counting.
func TestFormatLog_ToolResultRawFallback(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"user","message":{"content":` +
		`[{"type":"tool_result","content":42}]}}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "bytes=2") {
		t.Fatalf("missing bytes=2 from raw fallback: %q", got)
	}
}

// TestFormatLog_SystemNonInit pins the non-init system fall-through:
// future `system` subtypes survive as raw lines so we notice them.
func TestFormatLog_SystemNonInit(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"system","subtype":"warning",` +
		`"text":"x"}` + "\n")
	got := New().FormatLog(src)
	if string(got) != string(src) {
		t.Fatalf("FormatLog = %q, want raw %q", got, src)
	}
}
