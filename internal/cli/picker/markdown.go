package picker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/util/mdfile"
)

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
