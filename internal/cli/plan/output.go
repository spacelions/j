package plan

import (
	"path/filepath"
	"strings"
)

// planOutputPath returns the path where the plan should be written. It
// is derived from the target's basename so multiple inputs in the same
// directory each get their own output (e.g. "feature.md" -> "feature.plan.md",
// "1.md" -> "1.plan.md").
func planOutputPath(target string) string {
	base := filepath.Base(target)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(filepath.Dir(target), stem+".plan.md")
}
