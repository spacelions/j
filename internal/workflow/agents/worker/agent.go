// Package worker defines the worker sub-agent of the planner/worker/verifier
// workflow. It produces code from the plan (state key "plan") and any prior
// verifier feedback (state key "temp:review"), writing results under "code".
package worker

import (
	_ "embed"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
)

// Instruction is the embedded instruction.md used as the worker system
// prompt. Exported so other backends can reuse the same coding rules
// without duplicating the file.
//
//go:embed instruction.md
var Instruction string

const (
	Name      = "worker"
	OutputKey = "code"
)

// New returns a worker agent backed by the provided LLM.
func New(m model.LLM) (agent.Agent, error) {
	return llmagent.New(llmagent.Config{
		Name:        Name,
		Model:       m,
		Description: "Produces code from the plan, revising when verifier feedback is available.",
		Instruction: Instruction,
		OutputKey:   OutputKey,
	})
}
