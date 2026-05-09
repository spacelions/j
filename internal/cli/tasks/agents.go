package tasks

import (
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
)

func defaultAgents() []codingagents.Agent {
	return []codingagents.Agent{cursor.New(), claude.New(), deepseek.New()}
}
