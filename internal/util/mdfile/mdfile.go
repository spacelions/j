// Package mdfile provides a tiny helper for validating that a
// user-supplied path points at an existing markdown file. Both
// `j plan` (input task description) and `j work` (input plan markdown)
// share the same rules: trim whitespace, resolve to an absolute path,
// require a regular file with a .md or .markdown extension. Hosting
// the rules here keeps the two CLI commands in lock-step.
package mdfile

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

// Resolve validates that path points at an existing markdown file and
// returns the absolute path. Whitespace around the input is trimmed.
// The error messages name "markdown file" so the caller does not need
// to wrap them.
func Resolve(path string) (string, error) {
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
