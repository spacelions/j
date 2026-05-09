package claude

import "encoding/json"

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
// model every possible tool-input schema; it just renders the bytes.
type contentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
	IsError  bool            `json:"is_error"`
}
