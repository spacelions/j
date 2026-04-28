package work

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

// resolveTarget validates that path points at an existing plan markdown
// file and returns the absolute path. The rules mirror plan's
// resolveTarget but error messages reference "plan markdown" so users
// see why j work rejected a non-markdown input.
func resolveTarget(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("plan target is empty")
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
		return "", fmt.Errorf("%q is a directory, expected a plan markdown file", abs)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular file", abs)
	}
	ext := strings.ToLower(filepath.Ext(abs))
	if _, ok := markdownExts[ext]; !ok {
		return "", fmt.Errorf("%q is not a plan markdown file (expected .md or .markdown)", abs)
	}
	return abs, nil
}
