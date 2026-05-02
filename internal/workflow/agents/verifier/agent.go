// Package verifier defines the verifier sub-agent of the planner/worker/verifier
// workflow. It reviews the worker's output against the plan and writes a short
// verdict under the transient state key "temp:review".
package verifier

import (
	_ "embed"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
)

// Instruction is the embedded instruction.md used as the verifier system
// prompt. Exported so other backends can reuse the same review rules
// without duplicating the file.
//
//go:embed instruction.md
var Instruction string

const (
	Name      = "verifier"
	OutputKey = "temp:review"
)

// New returns a verifier agent backed by the provided LLM.
func New(m model.LLM) (agent.Agent, error) {
	return llmagent.New(llmagent.Config{
		Name:        Name,
		Model:       m,
		Description: "Reviews the worker's output against the plan and returns a concise verdict.",
		Instruction: Instruction,
		OutputKey:   OutputKey,
	})
}
