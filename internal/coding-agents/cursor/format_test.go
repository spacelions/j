package cursor

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
// UPDATE_GOLDEN=1 it (re)writes the golden file.
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
// fixture and returns the concatenated output.
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
// goldens stay stable across runs.
func normaliseTimestamps(s string) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		out.WriteString(rfc3339Z.ReplaceAllString(line, "TS  "))
	}
	return out.String()
}

// TestFormatLog_MalformedJSON pins the parse-failure fall-through:
// non-JSON input survives in agent.log verbatim.
func TestFormatLog_MalformedJSON(t *testing.T) {
	t.Parallel()
	in := []byte("panic: runtime error: bogus\n")
	got := New().FormatLog(in)
	if string(got) != string(in) {
		t.Fatalf("FormatLog = %q, want passthrough %q", got, in)
	}
}

// TestFormatLog_UnknownType pins the unknown-envelope fall-through.
// `connection`, `retry`, etc. fall through to the raw line so future
// envelope shapes stay visible without a code change.
func TestFormatLog_UnknownType(t *testing.T) {
	t.Parallel()
	in := []byte(`{"type":"connection",` +
		`"subtype":"reconnecting","attempt":1}` + "\n")
	got := New().FormatLog(in)
	if string(got) != string(in) {
		t.Fatalf("FormatLog = %q, want passthrough %q", got, in)
	}
}

// TestFormatLog_EmptyLine pins the empty-input branch.
func TestFormatLog_EmptyLine(t *testing.T) {
	t.Parallel()
	for _, in := range [][]byte{nil, []byte("\n"), {}} {
		got := New().FormatLog(in)
		if string(got) != string(in) {
			t.Fatalf("FormatLog(%q) = %q, want passthrough", in, got)
		}
	}
}

// TestFormatLog_ThinkingCompletedSuppressed pins the
// thinking/completed branch: the no-payload event yields no marker so
// agent.log is not cluttered with dangling lines.
func TestFormatLog_ThinkingCompletedSuppressed(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"thinking","subtype":"completed"}` + "\n")
	got := New().FormatLog(src)
	if len(got) != 0 {
		t.Fatalf("expected empty output, got %q", got)
	}
}

// TestFormatLog_TextTruncation pins the rune-cap behaviour.
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
}

// TestFormatLog_ToolCallStartedAndCompleted pins both subtypes:
// started emits `agent tool_use`, completed emits `agent tool_result`.
func TestFormatLog_ToolCallStartedAndCompleted(t *testing.T) {
	t.Parallel()
	started := []byte(`{"type":"tool_call","subtype":"started",` +
		`"tool_call":{"readToolCall":{"args":{"path":"/x"}}}}` + "\n")
	got := string(New().FormatLog(started))
	if !strings.Contains(got, "agent tool_use") {
		t.Fatalf("missing tool_use marker: %q", got)
	}
	if !strings.Contains(got, "name=read") {
		t.Fatalf("missing name=read: %q", got)
	}
	completed := []byte(`{"type":"tool_call","subtype":"completed",` +
		`"tool_call":{"readToolCall":{"args":{"path":"/x"},` +
		`"result":{"success":{"content":"ok"}}}}}` + "\n")
	got = string(New().FormatLog(completed))
	if !strings.Contains(got, "agent tool_result") {
		t.Fatalf("missing tool_result marker: %q", got)
	}
	if !strings.Contains(got, "ok=true") {
		t.Fatalf("missing ok=true: %q", got)
	}
}

// TestFormatLog_AssistantNonText pins the no-text-blocks fall-through:
// an assistant envelope whose blocks are all non-text yields the raw
// line so future block shapes stay visible.
func TestFormatLog_AssistantNonText(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"assistant","message":{"content":` +
		`[{"type":"image","data":"…"}]}}` + "\n")
	got := New().FormatLog(src)
	if string(got) != string(src) {
		t.Fatalf("FormatLog = %q, want raw %q", got, src)
	}
}

// TestFormatLog_UserEmptyContent pins the empty-content fall-through
// for the user prompt echo: a content array of [] yields the raw
// line.
func TestFormatLog_UserEmptyContent(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"user","message":{"content":[]}}` + "\n")
	got := New().FormatLog(src)
	if string(got) != string(src) {
		t.Fatalf("FormatLog = %q, want raw %q", got, src)
	}
}

// TestFormatLog_SystemNonInit pins the non-init system fall-through.
func TestFormatLog_SystemNonInit(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"system","subtype":"warning"}` + "\n")
	got := New().FormatLog(src)
	if string(got) != string(src) {
		t.Fatalf("FormatLog = %q, want raw %q", got, src)
	}
}

// TestFormatLog_ToolCallUnknownSubtype pins the default branch of
// renderToolCall: an unfamiliar subtype renders as a sparse
// `agent tool_call` marker so future shapes stay visible.
func TestFormatLog_ToolCallUnknownSubtype(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"tool_call","subtype":"queued",` +
		`"tool_call":{"readToolCall":{"args":{}}}}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "agent tool_call") {
		t.Fatalf("missing fallback marker: %q", got)
	}
}

// TestFormatLog_ToolCallNonObjectInner pins flattenToolCall's
// non-map branch: a `<X>ToolCall` whose value is not a map (e.g. a
// raw string) yields just the call name.
func TestFormatLog_ToolCallNonObjectInner(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"tool_call","subtype":"started",` +
		`"tool_call":{"readToolCall":"oops"}}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "name=read") {
		t.Fatalf("missing name=read: %q", got)
	}
}

func TestFormatLog_SystemInitOmitsEmptyFields(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"system","subtype":"init"}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "agent init") {
		t.Fatalf("missing init marker: %q", got)
	}
	for _, leak := range []string{"model=", "cwd="} {
		if strings.Contains(got, leak) {
			t.Fatalf("field %q leaked: %q", leak, got)
		}
	}
}

func TestFormatLog_AssistantEmptyContent(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"assistant","message":{"content":[]}}` +
		"\n")
	got := New().FormatLog(src)
	if string(got) != string(src) {
		t.Fatalf("FormatLog = %q, want raw %q", got, src)
	}
}

func TestFormatLog_UserNonText(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"user","message":{"content":` +
		`[{"type":"image","text":"ignored"}]}}` + "\n")
	got := New().FormatLog(src)
	if string(got) != string(src) {
		t.Fatalf("FormatLog = %q, want raw %q", got, src)
	}
}

func TestFormatLog_ToolCallLongArgsRenamesChars(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 250)
	src := []byte(`{"type":"tool_call","subtype":"started",` +
		`"tool_call":{"readToolCall":{"args":{"q":"` + long +
		`"}}}}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "input_chars=") {
		t.Fatalf("missing input_chars: %q", got)
	}
}

func TestFormatLog_ToolCallMissingArgs(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"tool_call","subtype":"started",` +
		`"tool_call":{"readToolCall":{"result":"ok"}}}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "name=read") {
		t.Fatalf("missing name=read: %q", got)
	}
}

func TestFlattenToolCall_Empty(t *testing.T) {
	t.Parallel()
	name, args, result := flattenToolCall(map[string]any{})
	if name != "" || args != "" || result != "" {
		t.Fatalf("flattenToolCall = (%q, %q, %q), want empty", name, args, result)
	}
}

func TestFormatLog_ResultSparse(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"result"}` + "\n")
	got := string(New().FormatLog(src))
	if !strings.Contains(got, "agent result") {
		t.Fatalf("missing result marker: %q", got)
	}
	if !strings.Contains(got, "ok=true") {
		t.Fatalf("missing ok=true: %q", got)
	}
	for _, leak := range []string{"subtype=", "duration_ms="} {
		if strings.Contains(got, leak) {
			t.Fatalf("field %q leaked: %q", leak, got)
		}
	}
}
