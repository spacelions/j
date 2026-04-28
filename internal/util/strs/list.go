// Package strs hosts string-shaped helpers shared across the codebase.
// It deliberately stays domain-free: callers pass in any context-specific
// noise filters or error wrappers.
package strs

import (
	"errors"
	"strings"
)

// ErrEmptyList is returned by ParseList when no items survive parsing.
// Callers wrap it with their own context (e.g. "cursor-agent returned no
// models").
var ErrEmptyList = errors.New("strs: empty list")

// TrimListPrefix strips a leading bullet ("- ", "* ", "• ") or numeric
// prefix ("1.", "23)") from s. Whitespace is trimmed from the result.
func TrimListPrefix(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if i := strings.IndexAny(s, ".)"); i > 0 {
		if isAllDigits(s[:i]) {
			s = strings.TrimSpace(s[i+1:])
		}
	}
	switch {
	case strings.HasPrefix(s, "- "):
		s = strings.TrimSpace(s[2:])
	case strings.HasPrefix(s, "* "):
		s = strings.TrimSpace(s[2:])
	case strings.HasPrefix(s, "• "):
		s = strings.TrimSpace(strings.TrimPrefix(s, "• "))
	}
	return s
}

// ParseList splits input on newlines into trimmed items. It skips:
//   - blank lines,
//   - lines whose lowercase form starts with any of noisePrefixes (used
//     to filter banner lines like "No models available."),
//   - lines that reduce to empty after TrimListPrefix.
//
// Returns ErrEmptyList if no items survive.
func ParseList(input string, noisePrefixes ...string) ([]string, error) {
	var items []string
	for _, raw := range strings.Split(input, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		skip := false
		for _, p := range noisePrefixes {
			if p != "" && strings.HasPrefix(lower, p) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		line = TrimListPrefix(line)
		if line == "" {
			continue
		}
		items = append(items, line)
	}
	if len(items) == 0 {
		return nil, ErrEmptyList
	}
	return items, nil
}

// isAllDigits reports whether every rune in s is an ASCII digit. Callers
// inside this package only invoke this with at least one character.
func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
