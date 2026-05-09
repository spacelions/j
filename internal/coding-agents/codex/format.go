package codex

// FormatLog implements codingagents.Agent for the codex backend.
// codex's `exec` entrypoint prints a multi-line human-readable trace
// (header block, user / codex turns, tokens-used footer) rather than
// stream-json, so the formatter is the identity transform — the bytes
// the child wrote already read like the rest of agent.log. Keeping
// the method satisfies the interface uniformly so the run helper can
// take a single `SpawnFormattedIn(..., a.FormatLog, ...)` code path
// for every backend.
func (*Agent) FormatLog(line []byte) []byte { return line }
