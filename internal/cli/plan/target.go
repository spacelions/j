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

// planOutputPath returns the path where the plan should be written.
func planOutputPath(target string) string {
	return filepath.Join(filepath.Dir(target), "plan.md")
}
