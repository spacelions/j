package claude

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/spacelions/j/internal/util/agentlog"
)

// formatTextLimit caps the runes copied from a text-bearing field
// (thinking, message, tool_use input, tool_result content) before the
// formatter elides the rest. 200 runes keeps each agent.log line
// scannable on a TTY without losing useful context for short turns.
const formatTextLimit = 200

// FormatLog turns one stream-json line emitted by claude's headless
// CLI into one or more agentlog-style marker lines. It parses the
// envelope's "type" field and routes onto a per-type renderer; if the
// JSON does not parse or the type is unknown the original line is
// returned untouched so panics / runtime errors / future event types
// still survive in agent.log verbatim. The returned bytes always end
// in '\n' so the run helper can write them straight through.
func (*Agent) FormatLog(line []byte) []byte {
	trimmed := bytes.TrimRight(line, "\r\n")
	if len(trimmed) == 0 {
		return line
	}
	var env envelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return line
	}
	switch env.Type {
	case "system":
		return renderSystem(env, line)
	case "assistant":
		return renderAssistant(env, line)
	case "user":
		return renderUserToolResult(env, line)
	case "result":
		return renderResult(env)
	default:
		return line
	}
}

// envelope captures every field FormatLog needs across the documented
// claude stream-json shapes. Unknown fields are ignored; missing
// fields decode to their zero value and the per-type renderer falls
// through to either a sparse marker or the raw line.
type envelope struct {
	Type    string   `json:"type"`
	Subtype string   `json:"subtype"`
	Model   string   `json:"model"`
	Tools   []string `json:"tools"`
	Message struct {
		Content []contentBlock `json:"content"`
	} `json:"message"`
	StopReason string `json:"stop_reason"`
	DurationMs int64  `json:"duration_ms"`
}

// contentBlock is one entry in claude's `message.content` array.
// Different block types populate different sub-fields; the formatter
// branches on Type and reads only what the branch needs. Input is
// kept as json.RawMessage so the formatter does not have to model
// every possible tool-input schema — it just renders the bytes.
type contentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
	IsError  bool            `json:"is_error"`
}

// renderSystem handles `{type:"system", subtype:"init", ...}`. Other
// system subtypes fall through to the raw line so future additions
// are visible without a code change.
func renderSystem(env envelope, raw []byte) []byte {
	if env.Subtype != "init" {
		return raw
	}
	fields := map[string]any{"model": env.Model}
	if len(env.Tools) > 0 {
		fields["tools"] = strings.Join(env.Tools, ",")
	}
	return marker("agent_init", fields)
}

// renderAssistant emits one marker per content block in the assistant
// message, preserving the order claude sent them. Empty content
// arrays fall through to the raw line so a malformed envelope is
// still visible.
func renderAssistant(env envelope, raw []byte) []byte {
	if len(env.Message.Content) == 0 {
		return raw
	}
	var buf bytes.Buffer
	for _, b := range env.Message.Content {
		buf.Write(renderBlock(b))
	}
	if buf.Len() == 0 {
		return raw
	}
	return buf.Bytes()
}

// renderBlock dispatches one assistant content block onto its marker
// renderer. Unknown block types fall through to a sparse `agent
// message` marker so agent.log stays readable even for unfamiliar
// shapes.
func renderBlock(b contentBlock) []byte {
	switch b.Type {
	case "thinking":
		return marker("agent_thinking", textFields(b.Thinking))
	case "text":
		return marker("agent_message", textFields(b.Text))
	case "tool_use":
		f := textFields(string(b.Input))
		f["name"] = b.Name
		// Rename truncated payload so input vs text reads correctly.
		f["input"] = f["text"]
		delete(f, "text")
		if cs, ok := f["chars"]; ok {
			f["input_chars"] = cs
			delete(f, "chars")
		}
		return marker("agent_tool_use", f)
	default:
		return marker("agent_message", textFields(b.Text))
	}
}

// renderUserToolResult emits one marker per tool_result block. claude
// wraps tool results inside a `user` envelope; non-tool_result user
// content (rare) falls through to the raw line.
func renderUserToolResult(env envelope, raw []byte) []byte {
	if len(env.Message.Content) == 0 {
		return raw
	}
	var buf bytes.Buffer
	matched := false
	for _, b := range env.Message.Content {
		if b.Type != "tool_result" {
			continue
		}
		matched = true
		body := toolResultBody(b.Content)
		buf.Write(marker("agent_tool_result", map[string]any{
			"ok":    !b.IsError,
			"bytes": len(body),
		}))
	}
	if !matched {
		return raw
	}
	return buf.Bytes()
}

// toolResultBody decodes a tool_result content payload. claude sends
// either a JSON string (most common) or an array of typed blocks; we
// flatten the array to the concatenation of its text-bearing fields
// for byte-counting only.
func toolResultBody(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var sb strings.Builder
		for _, bl := range blocks {
			sb.WriteString(bl.Text)
		}
		return sb.String()
	}
	return string(raw)
}

// renderResult handles the trailing `{type:"result", ...}` envelope.
// Missing fields are simply omitted from the marker so a sparse
// envelope still produces a readable line.
func renderResult(env envelope) []byte {
	fields := map[string]any{}
	if env.Subtype != "" {
		fields["subtype"] = env.Subtype
	}
	if env.StopReason != "" {
		fields["stop_reason"] = env.StopReason
	}
	if env.DurationMs > 0 {
		fields["duration_ms"] = env.DurationMs
	}
	return marker("agent_result", fields)
}

// textFields builds the common `text=<truncated> chars=<n>` field map
// used by thinking/message/tool_use blocks. Empty input yields an
// empty `text` slot which formatFields drops, so the marker still
// renders with whatever sibling fields the caller adds.
func textFields(s string) map[string]any {
	t, n, clipped := truncateRunes(s, formatTextLimit)
	out := map[string]any{"text": t}
	if clipped {
		out["chars"] = n
	}
	return out
}

// truncateRunes clips s to at most limit runes and reports the
// original rune count so the caller can surface "chars=<n>" alongside
// the truncated body. The clipped form gets a trailing "…" so a
// reader can tell content was elided.
func truncateRunes(s string, limit int) (string, int, bool) {
	runes := []rune(s)
	if len(runes) <= limit {
		return s, len(runes), false
	}
	return string(runes[:limit]) + "…", len(runes), true
}

// marker is agentlog.Render plus a trailing newline.
func marker(event string, fields map[string]any) []byte {
	return []byte(agentlog.Render(event, fields) + "\n")
}
