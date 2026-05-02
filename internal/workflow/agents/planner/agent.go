// Package planner defines the planner sub-agent of the planner/worker/verifier
// workflow. It reads the user's request and emits a concrete, ordered
// implementation plan under the state key "plan".
package planner

import (
	_ "embed"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
)

// Instruction is the embedded instruction.md used as the planner system
// prompt. It is exported so other coding-agent backends (Cursor, Codex,
// Claude, ...) can reuse the same planning rules instead of redefining
// their own copy.
//
//go:embed instruction.md
var Instruction string

// Name and OutputKey let workflow wiring reference this agent by symbol.
const (
	Name      = "planner"
	OutputKey = "plan"
)

// New returns a planner agent backed by the provided LLM.
func New(m model.LLM) (agent.Agent, error) {
	return llmagent.New(llmagent.Config{
		Name:        Name,
		Model:       m,
		Description: "Breaks the user's request into a concrete, ordered implementation plan.",
		Instruction: Instruction,
		OutputKey:   OutputKey,
	})
}
