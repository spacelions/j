package codereview

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// SchemaVersion is the current review.toml layout version. Bumped
// only when the on-disk shape changes in a way old j cannot read.
const SchemaVersion = 1

// Load reads and decodes review.toml at path.
func Load(path string) (ReviewFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ReviewFile{}, fmt.Errorf("codereview: read %q: %w", path, err)
	}
	var f ReviewFile
	if err := toml.Unmarshal(data, &f); err != nil {
		return ReviewFile{}, fmt.Errorf("codereview: decode %q: %w", path, err)
	}
	return f, nil
}

// Save writes f to path via atomic write+rename. The temp file lives
// alongside the target so the rename is filesystem-local (no cross-
// device move) and either the old or new file is visible at all
// times — readers never see a partial review.toml.
func Save(path string, f ReviewFile) error {
	data, err := toml.Marshal(f)
	if err != nil {
		return fmt.Errorf("codereview: encode %q: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("codereview: write %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("codereview: rename %q: %w", path, err)
	}
	return nil
}
