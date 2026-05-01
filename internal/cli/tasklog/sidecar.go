package tasklog

import (
	"os"
	"path/filepath"
	"strings"
)

// ReadRequirementSidecar derives the path to the original requirement
// markdown from a plan path produced by `j plan`'s legacy
// `<dir>/<stem>.plan.md` convention and returns its contents when
// readable. When the plan path does not follow this convention, or
// the sidecar file does not exist / cannot be read, an empty string
// is returned so the caller falls back to the plan body for the
// summary.
func ReadRequirementSidecar(planPath string) string {
	if planPath == "" {
		return ""
	}
	base := filepath.Base(planPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = strings.TrimSuffix(stem, ".plan")
	if stem == "" {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(planPath), stem+".md")
	if candidate == planPath {
		return ""
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		return ""
	}
	return string(data)
}
