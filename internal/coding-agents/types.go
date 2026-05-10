package codingagents

// AgentSession groups the selected backend identity and resume cursor.
type AgentSession struct {
	Tool     string
	Model    string
	ResumeID string
}
