// Package instructions exposes the embedded role-prompt markdown
// (planner / worker / verifier) as Go strings. The package is a
// dependency-free leaf — its only Go import is `embed` — so both
// the prompts renderer and the role agent constructors can share a
// single source of truth without forming an import cycle through
// the cli / coding-agents chain.
package instructions

import _ "embed"

// Planner is the embedded planner system prompt. Used by every
// backend (Cursor, Claude, the ADK LLMAgent) so planning rules
// stay in lockstep.
//
//go:embed planner.md
var Planner string

// Worker is the embedded worker system prompt. Used by every
// backend so coding rules stay in lockstep.
//
//go:embed worker.md
var Worker string

// Verifier is the embedded verifier system prompt. Used by every
// backend so review rules stay in lockstep.
//
//go:embed verifier.md
var Verifier string
