package agentlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// LineFormatter turns one input line (without trailing '\n') into
// zero or more output lines that should be appended to a per-task
// agent.log. Format handles stdout lines; FormatStderr handles
// stderr so a caller wiring two os.Pipe()s can route them through
// one formatter without losing the stdout/stderr distinction.
//
// The pipe-driven SpawnPipedIn helper hands each scanned line to a
// LineFormatter, then writes the returned slice atomically through
// WriteLines so concurrent stdout / stderr / marker writes cannot
// interleave mid-line.
type LineFormatter interface {
	Format(line []byte) [][]byte
	FormatStderr(line []byte) [][]byte
}

// PassThrough returns a formatter that emits non-empty stdout /
// stderr lines verbatim. It is the trivial implementation for
// callers that want SpawnPipedIn's serialized write path without
// any JSON parsing.
func PassThrough() LineFormatter { return passThrough{} }

type passThrough struct{}

func (passThrough) Format(line []byte) [][]byte {
	if len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	return [][]byte{cloneBytes(line)}
}

func (passThrough) FormatStderr(line []byte) [][]byte {
	if len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	return [][]byte{prefixed("stderr: ", line)}
}

// ClaudeStream parses Anthropic's `--output-format stream-json
// --verbose` events into `label: content` log lines (`session:`,
// `text:`, `thinking:`, `tool_use(...):`, `tool_result(...):`,
// `result:`). Unknown JSON / non-JSON lines round-trip as
// `unparsed: <raw>` so nothing is silently lost.
func ClaudeStream() LineFormatter { return jsonStream{} }

// CursorStream is the cursor-agent counterpart to ClaudeStream.
// cursor-agent's stream-json schema mirrors Anthropic's, so the two
// share the same parser; the named constructor exists so callers
// get a stable API even if the schemas drift apart later.
func CursorStream() LineFormatter { return jsonStream{} }

// jsonStream is the shared formatter used by ClaudeStream() and
// CursorStream(). It reads one stream-JSON event per line and emits
// a short labelled summary per content block; non-JSON / unknown
// events round-trip as `unparsed: <raw>` so nothing is silently
// lost.
type jsonStream struct{}

func (jsonStream) Format(line []byte) [][]byte {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] != '{' {
		return [][]byte{prefixed("unparsed: ", trimmed)}
	}
	var ev streamEvent
	if err := json.Unmarshal(trimmed, &ev); err != nil {
		return [][]byte{prefixed("unparsed: ", trimmed)}
	}
	return ev.render(trimmed)
}

func (jsonStream) FormatStderr(line []byte) [][]byte {
	if len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	return [][]byte{prefixed("stderr: ", line)}
}

// streamEvent is the shared decoded shape for Claude / Cursor
// stream-JSON events. Fields not present in a given event decode to
// their zero values; the renderer skips zero-value fields so the
// schemas can drift independently without breaking the formatter.
type streamEvent struct {
	Type       string         `json:"type"`
	Subtype    string         `json:"subtype"`
	Model      string         `json:"model"`
	Tools      []rawJSON      `json:"tools"`
	Message    *streamMessage `json:"message"`
	StopReason string         `json:"stop_reason"`
	NumTurns   int            `json:"num_turns"`
	Result     string         `json:"result"`
}

type streamMessage struct {
	Role    string          `json:"role"`
	Content []streamContent `json:"content"`
}

// streamContent is one block inside a `message.content` array. The
// same struct serves Anthropic and cursor-agent shapes: `text` /
// `thinking` carry a string, `tool_use` carries a name + JSON
// input, `tool_result` carries a tool_use_id + a content payload
// that may itself be a string or an array of `{type:text,text}`
// blocks.
type streamContent struct {
	Type      string  `json:"type"`
	Text      string  `json:"text"`
	Thinking  string  `json:"thinking"`
	Name      string  `json:"name"`
	Input     rawJSON `json:"input"`
	ToolUseID string  `json:"tool_use_id"`
	Content   rawJSON `json:"content"`
}

// rawJSON is json.RawMessage retyped so we can attach helpers
// without dragging unmarshal hooks across the file. UnmarshalJSON
// delegates to json.RawMessage so a missing field decodes to a nil
// slice (rather than failing the parent unmarshal).
type rawJSON json.RawMessage

func (r *rawJSON) UnmarshalJSON(b []byte) error {
	if r == nil {
		return fmt.Errorf("rawJSON: nil receiver")
	}
	*r = append((*r)[:0], b...)
	return nil
}

// render dispatches by event type. The raw bytes are threaded
// through so the unknown-event branch can still surface the
// original payload as `unparsed: <raw>` instead of dropping it on
// the floor.
func (ev streamEvent) render(raw []byte) [][]byte {
	switch ev.Type {
	case "system":
		return renderSystem(ev)
	case "assistant", "user":
		return renderMessage(ev)
	case "result":
		return renderResult(ev)
	default:
		return [][]byte{prefixed("unparsed: ", raw)}
	}
}

// renderSystem emits a single `session: model=<m> tools=<n>` line
// for the init event. Other system subtypes (rate-limit notices,
// mid-stream system messages) are dropped: agentlog's marker stream
// already records anything the orchestrator cares about, and a
// surprise system event would drown the human transcript.
func renderSystem(ev streamEvent) [][]byte {
	if ev.Subtype != "init" {
		return nil
	}
	parts := []string{"session:"}
	if ev.Model != "" {
		parts = append(parts, "model="+ev.Model)
	}
	if len(ev.Tools) > 0 {
		parts = append(parts,
			"tools="+strconv.Itoa(len(ev.Tools)))
	}
	if len(parts) == 1 {
		return nil
	}
	return [][]byte{[]byte(strings.Join(parts, " "))}
}

// renderMessage walks the message.content array and emits one line
// per recognised block. Unknown block types are skipped silently —
// they are usually new schema additions (image blocks, tool_use
// partials) that a human reader does not need to see in agent.log.
func renderMessage(ev streamEvent) [][]byte {
	if ev.Message == nil {
		return nil
	}
	var lines [][]byte
	for _, c := range ev.Message.Content {
		if line := renderContent(c); line != nil {
			lines = append(lines, line)
		}
	}
	return lines
}

func renderContent(c streamContent) []byte {
	switch c.Type {
	case "text":
		if c.Text == "" {
			return nil
		}
		return []byte("text: " + oneLine(c.Text))
	case "thinking":
		if c.Thinking == "" {
			return nil
		}
		return []byte("thinking: " + oneLine(c.Thinking))
	case "tool_use":
		input := strings.TrimSpace(string(c.Input))
		return []byte(fmt.Sprintf(
			"tool_use(%s): %s", c.Name, oneLine(input)))
	case "tool_result":
		body := stringifyToolResult(c.Content)
		return []byte(fmt.Sprintf(
			"tool_result(%s): %s", c.ToolUseID, oneLine(body)))
	}
	return nil
}

// stringifyToolResult flattens a tool_result content payload into a
// single string. Anthropic emits either a JSON string or an array
// of `{type:"text",text:...}` blocks (and rarely image blocks);
// cursor follows the same convention. We try the string shape
// first, then the block-array shape, then fall back to the raw
// JSON so a future schema addition still leaves something readable
// on disk.
func stringifyToolResult(raw rawJSON) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []streamContent
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var out []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				out = append(out, b.Text)
			}
		}
		if len(out) > 0 {
			return strings.Join(out, " ")
		}
	}
	return string(raw)
}

// renderResult emits a single `result: ...` line summarising the
// run. We include whichever of subtype, stop_reason, num_turns,
// and the final assistant text are present — Claude populates them
// all, cursor-agent only sets subtype + result.
func renderResult(ev streamEvent) [][]byte {
	parts := []string{"result:"}
	if ev.Subtype != "" {
		parts = append(parts, "subtype="+ev.Subtype)
	}
	if ev.StopReason != "" {
		parts = append(parts, "stop="+ev.StopReason)
	}
	if ev.NumTurns > 0 {
		parts = append(parts,
			"turns="+strconv.Itoa(ev.NumTurns))
	}
	if ev.Result != "" {
		parts = append(parts,
			"text="+oneLine(ev.Result))
	}
	if len(parts) == 1 {
		return nil
	}
	return [][]byte{[]byte(strings.Join(parts, " "))}
}

// oneLine collapses internal newlines into the literal `\n` escape
// so a multi-line thinking / tool_use / tool_result block still
// renders as a single agent.log line. Tab is left alone — `bat`,
// `tail`, and `less -R` all handle it.
func oneLine(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", `\n`)
}

// prefixed returns a fresh []byte of `prefix + line` so the
// caller's scanner buffer can be reused after we hand the result
// off to WriteLines.
func prefixed(prefix string, line []byte) []byte {
	out := make([]byte, 0, len(prefix)+len(line))
	out = append(out, prefix...)
	out = append(out, line...)
	return out
}

func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
