package plan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var markdownExts = map[string]struct{}{
	".md":       {},
	".markdown": {},
}

// resolveTarget validates that path points at an existing markdown file
// and returns the absolute path.
func resolveTarget(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("target is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", abs, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%q is a directory, expected a markdown file", abs)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular file", abs)
	}
	ext := strings.ToLower(filepath.Ext(abs))
	if _, ok := markdownExts[ext]; !ok {
		return "", fmt.Errorf("%q is not a markdown file (expected .md or .markdown)", abs)
	}
	return abs, nil
}

// planOutputPath returns the path where the plan should be written. It
// is derived from the target's basename so multiple inputs in the same
// directory each get their own output (e.g. "feature.md" -> "feature.plan.md",
// "1.md" -> "1.plan.md").
func planOutputPath(target string) string {
	base := filepath.Base(target)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(filepath.Dir(target), stem+".plan.md")
}
