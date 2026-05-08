package prompts

import (
	"strings"

	"github.com/spacelions/j/internal/agents/instructions"
)

// prependMustRead prefixes prompt with the project-wide must-read
// header followed by a bulleted list of files, then a blank line.
// The "read these files first" hint sits at the top of every
// composed prompt so the agent knows what to load before reading
// the role body and per-phase instructions.
//
// Empty / nil files returns prompt unchanged so existing
// "no must-read configured" output stays byte-identical.
//
// File names are rendered verbatim (no normalisation, no
// case-folding) because `AGENTS.md` and `agents.md` resolve to
// different files on case-sensitive filesystems.
func prependMustRead(prompt string, files []string) string {
	if len(files) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(instructions.MustReadHeader))
	b.WriteString("\n")
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("- ")
		b.WriteString(f)
	}
	b.WriteString("\n\n")
	b.WriteString(prompt)
	return b.String()
}
