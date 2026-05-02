package prompts

import "strings"

// mustreadSuffix renders the project-wide must-read list as a leading
// "\n\n"-prefixed bulleted block ready to be concatenated into a
// first-run planner / worker / verifier prompt. Empty or nil input
// returns "" so callers keep prompts byte-identical to the
// pre-mustread output when no files are configured. File names are
// rendered verbatim (no normalisation, no case-folding) because
// `AGENTS.md` and `agents.md` resolve to different files on
// case-sensitive filesystems.
func mustreadSuffix(files []string) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nBefore starting, read these project files for required context:\n")
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("- ")
		b.WriteString(f)
	}
	return b.String()
}
