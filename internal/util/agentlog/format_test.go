package agentlog

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

type formatCase struct {
	name string
	in   string
	want []string
}

func runFormatCases(
	t *testing.T, fmtr LineFormatter, cases []formatCase,
) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := fmtr.Format([]byte(c.in))
			gotStrings := bytesSlicesToStrings(got)
			if !reflect.DeepEqual(gotStrings, c.want) {
				t.Fatalf("Format(%q):\n got %#v\nwant %#v",
					c.in, gotStrings, c.want)
			}
		})
	}
}

func bytesSlicesToStrings(in [][]byte) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, b := range in {
		out[i] = string(b)
	}
	return out
}

func TestPassThrough_Format(t *testing.T) {
	t.Parallel()
	pt := PassThrough()
	cases := []formatCase{
		{"empty", "", nil},
		{"whitespace", "   \t  ", nil},
		{"raw", "hello world", []string{"hello world"}},
	}
	runFormatCases(t, pt, cases)
}

func TestPassThrough_FormatStderr(t *testing.T) {
	t.Parallel()
	pt := PassThrough()
	if got := pt.FormatStderr([]byte("")); got != nil {
		t.Fatalf("empty stderr should be dropped, got %v", got)
	}
	got := pt.FormatStderr([]byte("oops"))
	want := []string{"stderr: oops"}
	if !reflect.DeepEqual(bytesSlicesToStrings(got), want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestClaudeStream_AssistantBlocks(t *testing.T) {
	t.Parallel()
	fmtr := ClaudeStream()
	cases := []formatCase{
		{
			name: "text",
			in: `{"type":"assistant","message":{"content":[` +
				`{"type":"text","text":"hi"}]}}`,
			want: []string{"text: hi"},
		},
		{
			name: "thinking_multiline",
			in: `{"type":"assistant","message":{"content":[` +
				`{"type":"thinking",` +
				`"thinking":"line1\nline2"}]}}`,
			want: []string{`thinking: line1\nline2`},
		},
		{
			name: "tool_use",
			in: `{"type":"assistant","message":{"content":[` +
				`{"type":"tool_use","name":"Bash",` +
				`"input":{"command":"ls"}}]}}`,
			want: []string{
				`tool_use(Bash): {"command":"ls"}`,
			},
		},
		{
			name: "multiple_blocks",
			in: `{"type":"assistant","message":{"content":[` +
				`{"type":"thinking","thinking":"think"},` +
				`{"type":"text","text":"hi"}]}}`,
			want: []string{"thinking: think", "text: hi"},
		},
		{
			name: "empty_text_dropped",
			in: `{"type":"assistant","message":{"content":[` +
				`{"type":"text","text":""}]}}`,
			want: nil,
		},
		{
			name: "unknown_block_dropped",
			in: `{"type":"assistant","message":{"content":[` +
				`{"type":"image","source":{"data":"abc"}}]}}`,
			want: nil,
		},
		{
			name: "no_content_dropped",
			in:   `{"type":"assistant","message":{}}`,
			want: nil,
		},
		{
			name: "nil_message_dropped",
			in:   `{"type":"assistant"}`,
			want: nil,
		},
	}
	runFormatCases(t, fmtr, cases)
}

func TestClaudeStream_UserToolResult(t *testing.T) {
	t.Parallel()
	fmtr := ClaudeStream()
	cases := []formatCase{
		{
			name: "string_payload",
			in: `{"type":"user","message":{"content":[` +
				`{"type":"tool_result","tool_use_id":"abc",` +
				`"content":"file contents"}]}}`,
			want: []string{
				"tool_result(abc): file contents",
			},
		},
		{
			name: "block_array_payload",
			in: `{"type":"user","message":{"content":[` +
				`{"type":"tool_result","tool_use_id":"xyz",` +
				`"content":[{"type":"text","text":"a"},` +
				`{"type":"text","text":"b"}]}]}}`,
			want: []string{"tool_result(xyz): a b"},
		},
		{
			name: "empty_content_renders_blank",
			in: `{"type":"user","message":{"content":[` +
				`{"type":"tool_result","tool_use_id":"e"}]}}`,
			want: []string{"tool_result(e): "},
		},
		{
			name: "raw_falls_back_to_string",
			in: `{"type":"user","message":{"content":[` +
				`{"type":"tool_result","tool_use_id":"r",` +
				`"content":42}]}}`,
			want: []string{"tool_result(r): 42"},
		},
	}
	runFormatCases(t, fmtr, cases)
}

func TestClaudeStream_SystemAndResult(t *testing.T) {
	t.Parallel()
	fmtr := ClaudeStream()
	cases := []formatCase{
		{
			name: "system_init",
			in: `{"type":"system","subtype":"init",` +
				`"model":"claude-sonnet-4-6",` +
				`"tools":["Bash","Edit","Read"]}`,
			want: []string{
				"session: model=claude-sonnet-4-6 tools=3",
			},
		},
		{
			name: "system_init_no_fields_dropped",
			in:   `{"type":"system","subtype":"init"}`,
			want: nil,
		},
		{
			name: "system_other_subtype_dropped",
			in: `{"type":"system","subtype":"rate_limit",` +
				`"detail":"x"}`,
			want: nil,
		},
		{
			name: "result_full",
			in: `{"type":"result","subtype":"success",` +
				`"stop_reason":"end_turn","num_turns":2,` +
				`"result":"done"}`,
			want: []string{
				"result: subtype=success stop=end_turn " +
					"turns=2 text=done",
			},
		},
		{
			name: "result_minimal",
			in:   `{"type":"result","subtype":"success"}`,
			want: []string{"result: subtype=success"},
		},
		{
			name: "result_empty_dropped",
			in:   `{"type":"result"}`,
			want: nil,
		},
	}
	runFormatCases(t, fmtr, cases)
}

func TestClaudeStream_UnparsedAndDrop(t *testing.T) {
	t.Parallel()
	fmtr := ClaudeStream()
	cases := []formatCase{
		{"empty_drop", "", nil},
		{"whitespace_drop", "   ", nil},
		{
			name: "unknown_type",
			in:   `{"type":"rate_limit_event","ok":true}`,
			want: []string{
				`unparsed: {"type":"rate_limit_event","ok":true}`,
			},
		},
		{
			name: "non_json",
			in:   "plain text from a buggy CLI",
			want: []string{
				"unparsed: plain text from a buggy CLI",
			},
		},
		{
			name: "broken_json_starts_with_brace",
			in:   `{not even close to json`,
			want: []string{
				"unparsed: {not even close to json",
			},
		},
	}
	runFormatCases(t, fmtr, cases)
}

func TestClaudeStream_OversizedThinkingBlock(t *testing.T) {
	t.Parallel()
	fmtr := ClaudeStream()
	const size = 200_000
	body := strings.Repeat("a", size/2) + "\n" +
		strings.Repeat("b", size/2)
	in := []byte(`{"type":"assistant","message":{"content":[{` +
		`"type":"thinking","thinking":"`)
	in = append(in, []byte(body)...)
	in = append(in, []byte(`"}]}}`)...)
	got := fmtr.Format(in)
	if len(got) != 1 {
		t.Fatalf("want exactly one line, got %d", len(got))
	}
	out := got[0]
	if !bytes.HasPrefix(out, []byte("thinking: ")) {
		t.Fatalf("missing prefix")
	}
	if bytes.Count(out, []byte{'\n'}) != 0 {
		t.Fatal("oneLine must collapse internal newlines")
	}
	if !bytes.Contains(out, []byte(`\n`)) {
		t.Fatal("expected literal \\n escape in rendered line")
	}
}

func TestClaudeStream_FormatStderr(t *testing.T) {
	t.Parallel()
	fmtr := ClaudeStream()
	if got := fmtr.FormatStderr([]byte("")); got != nil {
		t.Fatalf("empty stderr should drop, got %v", got)
	}
	got := fmtr.FormatStderr([]byte("kaboom"))
	want := []string{"stderr: kaboom"}
	if !reflect.DeepEqual(bytesSlicesToStrings(got), want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCursorStream_SharesParserWithClaude(t *testing.T) {
	t.Parallel()
	fmtr := CursorStream()
	in := `{"type":"assistant","message":{"role":"assistant",` +
		`"content":[{"type":"text","text":"hi"}]}}`
	got := fmtr.Format([]byte(in))
	want := []string{"text: hi"}
	if !reflect.DeepEqual(bytesSlicesToStrings(got), want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWriteLines(t *testing.T) {
	t.Parallel()
	if err := WriteLines(nil, [][]byte{[]byte("x")}); err != nil {
		t.Fatalf("nil writer: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteLines(&buf, nil); err != nil {
		t.Fatalf("nil batch: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("nil batch wrote bytes: %q", buf.String())
	}
	err := WriteLines(&buf, [][]byte{
		[]byte("first"),
		[]byte("second\n"),
		[]byte("third"),
	})
	if err != nil {
		t.Fatalf("WriteLines: %v", err)
	}
	got := buf.String()
	want := "first\nsecond\nthird\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWriteLines_WriteError(t *testing.T) {
	t.Parallel()
	err := WriteLines(failingWriter{}, [][]byte{[]byte("x")})
	if err == nil {
		t.Fatal("expected write error")
	}
	if !strings.Contains(err.Error(), "write line") {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteLines_NewlineWriteError(t *testing.T) {
	t.Parallel()
	w := &nthWriteFails{failOn: 2}
	err := WriteLines(w, [][]byte{[]byte("noNL")})
	if err == nil {
		t.Fatal("expected newline write error")
	}
	if !strings.Contains(err.Error(), "write newline") {
		t.Fatalf("err = %v", err)
	}
}

type nthWriteFails struct {
	failOn int
	calls  int
}

func (n *nthWriteFails) Write(p []byte) (int, error) {
	n.calls++
	if n.calls >= n.failOn {
		return 0, errBoom
	}
	return len(p), nil
}
