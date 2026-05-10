package codex

import "encoding/json"

// codexEnvelope captures the top-level `codex exec --json` events
// FormatLog renders. Unknown fields decode silently and unknown event
// types fall through to the raw line.
type codexEnvelope struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Usage    codexUsage      `json:"usage"`
	Error    *codexError     `json:"error"`
	Message  string          `json:"message"`
	Item     json.RawMessage `json:"item"`
}

type codexUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

type codexError struct {
	Message string `json:"message"`
}

type codexItem struct {
	Type             string            `json:"type"`
	Message          string            `json:"message"`
	Text             string            `json:"text"`
	Command          string            `json:"command"`
	AggregatedOutput string            `json:"aggregated_output"`
	ExitCode         *int              `json:"exit_code"`
	Status           string            `json:"status"`
	Changes          []codexFileChange `json:"changes"`
	Server           string            `json:"server"`
	Tool             string            `json:"tool"`
	Error            *codexError       `json:"error"`
	Query            string            `json:"query"`
	Action           json.RawMessage   `json:"action"`
	Items            []codexTodoItem   `json:"items"`
}

type codexFileChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type codexTodoItem struct {
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
}
