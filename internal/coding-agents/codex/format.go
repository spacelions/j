package codex

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/spacelions/j/internal/util/agentlog"
)

const formatTextLimit = 200

// FormatLog turns one JSONL event emitted by `codex exec --json` into
// one concise agentlog marker line. Unparseable input, unknown event
// types, and unknown item types fall through verbatim so future Codex
// events and process crashes remain visible in agent.log.
func (*Agent) FormatLog(line []byte) []byte {
	trimmed := bytes.TrimRight(line, "\r\n")
	if len(trimmed) == 0 {
		return line
	}
	var env codexEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return line
	}
	switch env.Type {
	case "thread.started":
		return marker("agent_thread", map[string]any{
			"thread_id": env.ThreadID,
		})
	case "turn.started":
		return marker("agent_status", map[string]any{
			"status": "turn_started",
		})
	case "turn.completed":
		return renderTurnCompleted(env)
	case "turn.failed":
		return marker("agent_error", map[string]any{
			"message": errorMessage(env),
		})
	case "error":
		return marker("agent_error", map[string]any{
			"message": errorMessage(env),
		})
	case "item.started", "item.updated", "item.completed":
		return renderItemEvent(env, line)
	default:
		return line
	}
}

func renderTurnCompleted(env codexEnvelope) []byte {
	fields := map[string]any{}
	putInt(fields, "input_tokens", env.Usage.InputTokens)
	putInt(fields, "cached_input_tokens", env.Usage.CachedInputTokens)
	putInt(fields, "output_tokens", env.Usage.OutputTokens)
	putInt(
		fields,
		"reasoning_output_tokens",
		env.Usage.ReasoningOutputTokens,
	)
	return marker("agent_result", fields)
}

func renderItemEvent(env codexEnvelope, raw []byte) []byte {
	var item codexItem
	if err := json.Unmarshal(env.Item, &item); err != nil {
		return raw
	}
	phase := strings.TrimPrefix(env.Type, "item.")
	switch item.Type {
	case "agent_message":
		return marker("agent_message", textFields(item.Text))
	case "reasoning":
		return marker("agent_thinking", textFields(item.Text))
	case "command_execution":
		return renderCommand(item, phase)
	case "file_change":
		return renderFileChange(item, phase)
	case "mcp_tool_call":
		return renderMCPToolCall(item, phase)
	case "web_search":
		return renderWebSearch(item, phase)
	case "todo_list":
		return renderTodoList(item, phase)
	case "error":
		return marker("agent_error", map[string]any{
			"message": itemErrorMessage(item),
		})
	default:
		return raw
	}
}

func renderCommand(item codexItem, phase string) []byte {
	fields := textFields(item.Command)
	renameTextFields(fields, "command")
	fields["phase"] = phase
	putString(fields, "status", item.Status)
	if item.ExitCode != nil {
		fields["exit_code"] = *item.ExitCode
	}
	if item.AggregatedOutput != "" {
		fields["output_bytes"] = len(item.AggregatedOutput)
	}
	return marker("agent_command", fields)
}

func renderFileChange(item codexItem, phase string) []byte {
	fields := map[string]any{
		"changes": len(item.Changes),
		"phase":   phase,
	}
	putString(fields, "status", item.Status)
	putString(fields, "files", changeSummary(item.Changes))
	return marker("agent_file_change", fields)
}

func renderMCPToolCall(item codexItem, phase string) []byte {
	fields := map[string]any{
		"phase": phase,
	}
	putString(fields, "server", item.Server)
	putString(fields, "tool", item.Tool)
	putString(fields, "status", item.Status)
	if item.Error != nil {
		putString(fields, "message", trunc(item.Error.Message))
	}
	return marker("agent_mcp_tool_call", fields)
}

func renderWebSearch(item codexItem, phase string) []byte {
	fields := textFields(item.Query)
	renameTextFields(fields, "query")
	fields["phase"] = phase
	putString(fields, "action", webSearchAction(item.Action))
	return marker("agent_web_search", fields)
}

func renderTodoList(item codexItem, phase string) []byte {
	total := len(item.Items)
	completed := 0
	current := ""
	for _, todo := range item.Items {
		if todo.Completed {
			completed++
			continue
		}
		if current == "" {
			current = todo.Text
		}
	}
	fields := textFields(current)
	renameTextFields(fields, "current")
	fields["phase"] = phase
	fields["items"] = total
	fields["completed"] = completed
	fields["pending"] = total - completed
	return marker("agent_todo_list", fields)
}

func changeSummary(changes []codexFileChange) string {
	limit := min(len(changes), 5)
	parts := make([]string, 0, limit+1)
	for _, change := range changes[:limit] {
		parts = append(parts, change.Kind+":"+change.Path)
	}
	if len(changes) > limit {
		parts = append(parts, "+"+strconv.Itoa(len(changes)-limit))
	}
	return trunc(strings.Join(parts, ","))
}

func webSearchAction(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Type
	}
	return ""
}

func errorMessage(env codexEnvelope) string {
	if env.Error != nil {
		return trunc(env.Error.Message)
	}
	return trunc(env.Message)
}

func itemErrorMessage(item codexItem) string {
	if item.Error != nil {
		return trunc(item.Error.Message)
	}
	return trunc(item.Message)
}

func renameTextFields(fields map[string]any, name string) {
	if v, ok := fields["text"]; ok {
		fields[name] = v
		delete(fields, "text")
	}
	if v, ok := fields["chars"]; ok {
		fields[name+"_chars"] = v
		delete(fields, "chars")
	}
}

func putString(fields map[string]any, key, value string) {
	if value != "" {
		fields[key] = value
	}
}

func putInt(fields map[string]any, key string, value int64) {
	if value != 0 {
		fields[key] = value
	}
}

func textFields(s string) map[string]any {
	t, n, clipped := truncateRunes(s, formatTextLimit)
	fields := map[string]any{"text": t}
	if clipped {
		fields["chars"] = n
	}
	return fields
}

func trunc(s string) string {
	t, _, _ := truncateRunes(s, formatTextLimit)
	return t
}

func truncateRunes(s string, limit int) (string, int, bool) {
	runes := []rune(s)
	if len(runes) <= limit {
		return s, len(runes), false
	}
	return string(runes[:limit]) + "...", len(runes), true
}

func marker(event string, fields map[string]any) []byte {
	return []byte(agentlog.Render(event, fields) + "\n")
}
