package linear

import (
	"fmt"
	"strings"
)

// IssueToMarkdown turns a fetched Linear issue into the body `j`
// writes to requirements.md. The shape is fixed:
//
//	# <title>
//
//	<description>   ← block omitted entirely when description is empty
//
//	---
//	Linear: <url>
//
// Title / description are trimmed before rendering so leading or
// trailing whitespace from Linear's API never bleeds into the
// requirements file.
func IssueToMarkdown(issue Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", strings.TrimSpace(issue.Title))
	desc := strings.TrimSpace(issue.Description)
	if desc != "" {
		b.WriteString("\n")
		b.WriteString(desc)
		b.WriteString("\n")
	}
	b.WriteString("\n---\n")
	fmt.Fprintf(&b, "Linear: %s\n", issue.URL)
	return b.String()
}
