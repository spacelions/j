package settings

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/agents/prompts"
	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store"
)

// maybeSeedPromptFile is the copy-on-set hook for `j settings set
// <role>.prompt=<path>`. When the entry targets a planner / worker /
// verifier prompt and the destination file does not yet exist, the
// embedded role markdown is written there (parents created). An
// existing file is left untouched so user edits survive a re-`set`.
//
// A success line is echoed to stdout via uitheme.NormalFprintf so the
// user knows where the seed file landed; the caller (runSet) emits
// its own `set <bucket>.<key> = <value>` line afterwards.
//
// Returns nil for non-prompt entries so runSet's loop can call this
// unconditionally.
func maybeSeedPromptFile(out io.Writer, e setEntry) error {
	if e.key != store.KeyPromptPath || !store.IsRoleBucket(e.bucket) {
		return nil
	}
	body := prompts.EmbeddedDefault(e.bucket)
	abs, _ := filepath.Abs(e.value)
	if _, err := os.Stat(abs); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("settings: stat %q: %w", abs, err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("settings: mkdir %q: %w",
			filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		return fmt.Errorf("settings: write %q: %w", abs, err)
	}
	uitheme.NormalFprintf(out,
		"J: wrote default prompt to %s\n", abs)
	return nil
}
