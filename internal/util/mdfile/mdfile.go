// Package mdfile provides tiny helpers for working with the markdown
// files that drive the `j plan` and `j work` flows. Both commands
// share the same Resolve rules: trim whitespace, resolve to an
// absolute path, require a regular file with a .md or .markdown
// extension. ListInDir is the directory-scan companion used by
// `j plan`'s markdown source picker. Hosting the rules here keeps the
// two CLI commands in lock-step.
package mdfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var markdownExts = map[string]struct{}{
	".md":       {},
	".markdown": {},
}

// excludedMarkdownBasenames lists the lower-cased basenames that
// ListInDir hides from its result regardless of extension. The set
// is fixed (no runtime tweaks, no env overrides) so the picker is
// stable across projects: AGENTS.md and README.md are repository
// chrome rather than plannable specs and surfacing them confuses
// the user. The keys are lower-cased so the match is
// case-insensitive (`AGENTS.md`, `agents.md`, `Agents.MD` all hit).
var excludedMarkdownBasenames = map[string]struct{}{
	"agents.md": {},
	"readme.md": {},
	"claude.md": {},
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

// ListInDir returns the absolute paths of the markdown files directly
// inside dir, in case-insensitive basename order. The scan is
// non-recursive: subdirectories are skipped silently. A file is
// included iff it is a regular file (no FIFOs, sockets, or
// directory-entry symlinks) with a .md / .markdown extension
// (case-insensitive against markdownExts), its basename does not
// start with `.` (hidden files like `.draft.md` are scratch space we
// never want to surface), and its lower-cased basename is not in the
// fixed excludedMarkdownBasenames allowlist (AGENTS.md / README.md).
//
// Errors from os.ReadDir propagate verbatim. A directory with no
// matching entries returns (nil, nil) so the caller decides how to
// surface the empty result; the `j plan` orchestrator turns it into
// a single user-facing error mentioning the cwd.
func ListInDir(dir string) ([]string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", dir, err)
	}
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !markdownEntryIncluded(e, name) {
			continue
		}
		out = append(out, filepath.Join(absDir, name))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(filepath.Base(out[i])) < strings.ToLower(filepath.Base(out[j]))
	})
	return out, nil
}

// markdownEntryIncluded encapsulates the inclusion rules for
// ListInDir so the loop body stays linear. Any non-regular file
// (directory, FIFO, socket, symlink-to-dir) drops out here, as do
// hidden basenames (`.foo.md`), non-markdown extensions, and the
// fixed AGENTS.md / README.md exclusion set.
func markdownEntryIncluded(e os.DirEntry, name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	if !e.Type().IsRegular() {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	if _, ok := markdownExts[ext]; !ok {
		return false
	}
	if _, excluded := excludedMarkdownBasenames[strings.ToLower(name)]; excluded {
		return false
	}
	return true
}
