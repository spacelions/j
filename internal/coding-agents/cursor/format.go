package cursor

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/spacelions/j/internal/util/agentlog"
)

// formatTextLimit caps the runes copied from a text-bearing field
// before the formatter elides the rest. 200 keeps each agent.log line
// scannable on a TTY without losing useful context for short turns.
const formatTextLimit = 200

// FormatLog turns one stream-json line emitted by cursor-agent's
// headless CLI into one or more agentlog-style marker lines. Cursor's
// envelope uses a top-level `type` and (for tool / thinking events) a
// `subtype` field; the formatter routes on those and falls through to
// the raw line for unknown types so panics / runtime errors / future
// event shapes still survive in agent.log verbatim.
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
	case "user":
		return renderUser(env, line)
	case "assistant":
		return renderAssistant(env, line)
	case "tool_call":
		return renderToolCall(env)
	case "thinking":
		return renderThinking(env)
	case "result":
		return renderResult(env)
	default:
		return line
	}
}

// envelope captures every field FormatLog needs across the cursor
// stream-json shapes. Unknown top-level fields decode silently;
// missing fields decode to their zero value and the per-type renderer
// falls through to a sparse marker or the raw line.
type envelope struct {
	Type     string         `json:"type"`
	Subtype  string         `json:"subtype"`
	Cwd      string         `json:"cwd"`
	Model    string         `json:"model"`
	Text     string         `json:"text"`
	IsError  bool           `json:"is_error"`
	Duration int64          `json:"duration_ms"`
	Message  cursorMessage  `json:"message"`
	ToolCall map[string]any `json:"tool_call"`
}

// cursorMessage matches the inner `message` envelope cursor wraps
// around user / assistant text content. Each block carries its own
// `type` ("text" today) so future block types can be folded in.
type cursorMessage struct {
	Content []cursorBlock `json:"content"`
}

// cursorBlock is one entry in `message.content`. Today only `text` is
// observed; the formatter still branches on Type so unfamiliar block
// shapes can be handled without re-parsing.
type cursorBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// renderSystem handles `{type:"system", subtype:"init", ...}`. Other
// system subtypes fall through to the raw line so future additions
// stay visible without a code change.
func renderSystem(env envelope, raw []byte) []byte {
	if env.Subtype != "init" {
		return raw
	}
	fields := map[string]any{}
	if env.Model != "" {
		fields["model"] = env.Model
	}
	if env.Cwd != "" {
		fields["cwd"] = env.Cwd
	}
	return marker("agent_init", fields)
}

// renderUser emits `agent user — text=<truncated>` for the prompt
// echo cursor prints back at the start of a turn. Empty content
// arrays fall through to the raw line.
func renderUser(env envelope, raw []byte) []byte {
	t, ok := firstText(env.Message.Content)
	if !ok {
		return raw
	}
	return marker("agent_user", textFields(t))
}

// renderAssistant emits one `agent message` marker per text block in
// the assistant message. With `--stream-partial-output` dropped from
// argv the CLI emits one event per complete text block, so the loop
// usually fires once per envelope.
func renderAssistant(env envelope, raw []byte) []byte {
	if len(env.Message.Content) == 0 {
		return raw
	}
	var buf bytes.Buffer
	matched := false
	for _, b := range env.Message.Content {
		if b.Type != "text" {
			continue
		}
		matched = true
		buf.Write(marker("agent_message", textFields(b.Text)))
	}
	if !matched {
		return raw
	}
	return buf.Bytes()
}

// renderToolCall handles cursor's tool_call/started and
// tool_call/completed envelopes. The structured tool_call payload
// nests one `<name>ToolCall: {args, result?}` map; the formatter
// surfaces the call name, args (truncated), and on completion the
// raw byte length of the result.
func renderToolCall(env envelope) []byte {
	name, args, result := flattenToolCall(env.ToolCall)
	switch env.Subtype {
	case "started":
		f := textFields(args)
		f["name"] = name
		f["input"] = f["text"]
		delete(f, "text")
		if c, ok := f["chars"]; ok {
			f["input_chars"] = c
			delete(f, "chars")
		}
		return marker("agent_tool_use", f)
	case "completed":
		f := map[string]any{
			"name":  name,
			"ok":    !env.IsError,
			"bytes": len(result),
		}
		return marker("agent_tool_result", f)
	default:
		return marker("agent_tool_call", map[string]any{
			"name": name, "subtype": env.Subtype,
		})
	}
}

// flattenToolCall pulls the call name, args bytes, and result bytes
// out of cursor's `tool_call` map. The map has exactly one key of
// the form `<name>ToolCall` whose value is `{args, result?}`. The
// returned bytes are JSON-encoded so renderToolCall can hand them to
// textFields without re-encoding.
func flattenToolCall(tc map[string]any) (name, args, result string) {
	for k, v := range tc {
		name = strings.TrimSuffix(k, "ToolCall")
		inner, ok := v.(map[string]any)
		if !ok {
			return name, "", ""
		}
		if a, ok := inner["args"]; ok {
			b, _ := json.Marshal(a)
			args = string(b)
		}
		if r, ok := inner["result"]; ok {
			b, _ := json.Marshal(r)
			result = string(b)
		}
		return name, args, result
	}
	return "", "", ""
}

// renderThinking handles cursor's `thinking/delta` and
// `thinking/completed` envelopes. The completed event carries no
// payload and is suppressed (returning a single newline) to keep
// agent.log uncluttered; deltas surface the chunk's text.
func renderThinking(env envelope) []byte {
	if env.Subtype == "completed" {
		return []byte{}
	}
	return marker("agent_thinking", textFields(env.Text))
}

// renderResult handles the trailing `{type:"result", ...}` envelope.
// Missing fields are simply omitted from the marker so a sparse
// envelope still produces a readable line.
func renderResult(env envelope) []byte {
	fields := map[string]any{}
	if env.Subtype != "" {
		fields["subtype"] = env.Subtype
	}
	if env.Duration > 0 {
		fields["duration_ms"] = env.Duration
	}
	fields["ok"] = !env.IsError
	return marker("agent_result", fields)
}

// firstText returns the first text block's body, or false if the
// message has no text content.
func firstText(blocks []cursorBlock) (string, bool) {
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text, true
		}
	}
	return "", false
}

// textFields builds the common `text=<truncated> chars=<n>` map.
// Empty input yields an empty `text` slot which agentlog drops, so
// the marker still renders with whatever sibling fields the caller
// adds.
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
