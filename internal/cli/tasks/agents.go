package tasks

import (
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
)

// defaultAgents returns the picker order shown in every interactive
// prompt. Order matters: the first entry is the default highlight a
// user sees when they have not stored a per-task / per-role pick.
// claude leads because it is the most commonly used backend on the
// team; codex follows as the second OpenAI-flavoured option, then
// deepseek, then cursor.
func defaultAgents() []codingagents.Agent {
	return []codingagents.Agent{
		claude.New(), codex.New(), deepseek.New(), cursor.New(),
	}
}
