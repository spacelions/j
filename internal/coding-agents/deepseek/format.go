package deepseek

// FormatLog implements codingagents.Agent for the deepseek backend.
// deepseek-tui prints its full reasoning + tool-call trace as plain
// human text rather than stream-json, so the formatter is the
// identity transform — the bytes the child wrote already read like
// the rest of agent.log. Keeping the method satisfies the interface
// uniformly so the run helper can take a single
// `SpawnFormattedIn(..., a.FormatLog, ...)` code path for every
// backend.
func (*Agent) FormatLog(line []byte) []byte { return line }
