// Package verifier defines the verifier sub-agent of the planner/coder/verifier
// workflow. It reviews the coder's output against the plan and writes a short
// verdict under the transient state key "temp:review".
package verifier

import (
	_ "embed"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
)

//go:embed instruction.md
var instruction string

const (
	Name      = "verifier"
	OutputKey = "temp:review"
)

// New returns a verifier agent backed by the provided LLM.
func New(m model.LLM) (agent.Agent, error) {
	return llmagent.New(llmagent.Config{
		Name:        Name,
		Model:       m,
		Description: "Reviews the coder's output against the plan and returns a concise verdict.",
		Instruction: instruction,
		OutputKey:   OutputKey,
	})
}
