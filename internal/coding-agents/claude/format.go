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
// CLI into one or more agentlog-style marker lines. Unparseable input
// or unknown types fall through verbatim so panics / future event
// types still survive in agent.log. Returned bytes always end in '\n'.
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
// claude stream-json shapes. Missing fields decode to zero and the
// per-type renderer falls through to a sparse marker or the raw line.
type envelope struct {
	Type    string   `json:"type"`
	Subtype string   `json:"subtype"`
	Model   string   `json:"model"`
	Tools   []string `json:"tools"`
	Message struct {
		Content []contentBlock `json:"content"`
	} `json:"message"`
	StopReason   string    `json:"stop_reason"`
	DurationMs   int64     `json:"duration_ms"`
	TaskID       string    `json:"task_id"`
	Description  string    `json:"description"`
	TaskType     string    `json:"task_type"`
	Prompt       string    `json:"prompt"`
	Status       string    `json:"status"`
	Summary      string    `json:"summary"`
	LastToolName string    `json:"last_tool_name"`
	Usage        taskUsage `json:"usage"`
}

// taskUsage mirrors the `usage` object claude attaches to
// `task_progress` and `task_notification` system events.
type taskUsage struct {
	TotalTokens int64 `json:"total_tokens"`
	ToolUses    int64 `json:"tool_uses"`
	DurationMs  int64 `json:"duration_ms"`
}

// contentBlock is one entry in claude's `message.content` array.
// Input is kept as json.RawMessage so the formatter does not have to
// model every possible tool-input schema — it just renders the bytes.
type contentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
	IsError  bool            `json:"is_error"`
}

// renderSystem dispatches on subtype. Known subtypes render as a
// marker; unknown subtypes fall through to the raw line so future
// additions stay visible without a code change.
func renderSystem(env envelope, raw []byte) []byte {
	switch env.Subtype {
	case "init":
		f := map[string]any{"model": env.Model}
		if len(env.Tools) > 0 {
			f["tools"] = strings.Join(env.Tools, ",")
		}
		return marker("agent_init", f)
	case "task_started":
		f := textFields(env.Prompt)
		renameTextFields(f, "prompt")
		putString(f, "task_id", env.TaskID)
		putString(f, "description", env.Description)
		putString(f, "task_type", env.TaskType)
		return marker("agent_subtask_start", f)
	case "task_progress":
		f := textFields(env.Description)
		renameTextFields(f, "description")
		putString(f, "task_id", env.TaskID)
		putString(f, "last_tool_name", env.LastToolName)
		putUsage(f, env.Usage)
		return marker("agent_subtask_progress", f)
	case "task_notification":
		f := textFields(env.Summary)
		renameTextFields(f, "summary")
		putString(f, "task_id", env.TaskID)
		putString(f, "status", env.Status)
		putUsage(f, env.Usage)
		return marker("agent_subtask_done", f)
	case "status":
		f := map[string]any{}
		putString(f, "status", env.Status)
		return marker("agent_status", f)
	default:
		return raw
	}
}

// renameTextFields swaps the "text"/"chars" keys produced by
// textFields for caller-specific names (prompt/description/summary).
func renameTextFields(f map[string]any, name string) {
	if v, ok := f["text"]; ok {
		f[name] = v
		delete(f, "text")
	}
	if v, ok := f["chars"]; ok {
		f[name+"_chars"] = v
		delete(f, "chars")
	}
}

// putString / putInt drop empties at the call site so int64(0) does
// not surface as `key=0` (formatValue does not treat it as empty).
func putString(f map[string]any, k, v string) {
	if v != "" {
		f[k] = v
	}
}

func putInt(f map[string]any, k string, v int64) {
	if v != 0 {
		f[k] = v
	}
}

// putUsage spreads a sub-agent usage block onto f, skipping zeroes.
func putUsage(f map[string]any, u taskUsage) {
	putInt(f, "tool_uses", u.ToolUses)
	putInt(f, "total_tokens", u.TotalTokens)
	putInt(f, "duration_ms", u.DurationMs)
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
		renameTextFields(f, "input")
		f["name"] = b.Name
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

// truncateRunes clips s to at most limit runes, returning the
// original rune count and a "…" suffix when content was elided.
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
