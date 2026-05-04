package picker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/util/mdfile"
)

// ErrEmptyFromFile is returned by AskFromFile when the user submits
// an empty / whitespace-only path. Exported as a sentinel so callers
// can distinguish the "user cleared the input" case from genuine UI
// failures via errors.Is.
var ErrEmptyFromFile = errors.New("picker: no markdown provided")

// PickMarkdownInCwd scans the current working directory for markdown
// files via mdfile.ListInDir (`AGENTS.md` / `README.md` / hidden /
// non-regular entries are excluded), renders the basename picker, and
// resolves the chosen basename back to its absolute path. An empty
// cwd surfaces a wrapped error mentioning the directory so the cli
// does not have to translate it.
func (p *Picker) PickMarkdownInCwd(ctx context.Context) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("picker: getwd: %w", err)
	}
	abs, err := mdfile.ListInDir(cwd)
	if err != nil {
		return "", fmt.Errorf("picker: scan %s: %w", cwd, err)
	}
	if len(abs) == 0 {
		return "", fmt.Errorf("picker: no markdown files in %s (excluding AGENTS.md/README.md)", cwd)
	}
	basenames := make([]string, len(abs))
	byBase := make(map[string]string, len(abs))
	for i, path := range abs {
		base := filepath.Base(path)
		basenames[i] = base
		byBase[base] = path
	}
	chosen, err := p.choose(ctx, "Select markdown file", basenames)
	if err != nil {
		return "", err
	}
	target, ok := byBase[chosen]
	if !ok {
		return "", fmt.Errorf("picker: unknown markdown selection %q", chosen)
	}
	return target, nil
}

// AskFromFile renders the legacy free-text input fallback used by
// `j work` / `j verify` when cwd has no plan-done tasks AND no
// `--from-file` was supplied. Empty / whitespace-only input returns
// ErrEmptyFromFile so the caller can surface a clean "J: no markdown
// provided" message without re-checking the value.
func (p *Picker) AskFromFile(ctx context.Context) (string, error) {
	var v string
	if err := p.run(ctx, huh.NewInput().
		Title("Plan markdown file location").
		Placeholder("/path/to/feature.plan.md").
		Value(&v)); err != nil {
		return "", err
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", ErrEmptyFromFile
	}
	return v, nil
}
