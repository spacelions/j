package workflow

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/spacelions/j/internal/store"
)

// Sentinel errors returned by LoadConfig when a required project
// setting is missing or unreadable. Each message tells the user the
// exact `j settings set project.<key>=<value>` command to run.
var (
	ErrMissingAPIKey        = errors.New("project.api_key is not set; run `j settings set project.api_key=<your-google-api-key>`")
	ErrMissingModel         = errors.New("project.model is not set; run `j settings set project.model=<gemini-model>` (e.g. gemini-2.5-flash)")
	ErrMissingMaxIterations = errors.New("project.max_iterations is not set or is invalid; run `j settings set project.max_iterations=<positive-integer>`")
)

// Config bundles the runtime knobs consumed by the workflow. It lives
// in the workflow package so callers don't need a separate import path
// just to construct one for tests or `j run` / `j web`.
type Config struct {
	// APIKey is the Gemini / Google API key.
	APIKey string
	// Model is the Gemini model name (e.g. "gemini-2.5-flash").
	Model string
	// MaxIterations bounds the coder/verifier loop.
	MaxIterations uint
}

// LoadConfig reads the three runtime knobs from the per-project bbolt
// settings store at <cwd>/.j/settings. A missing file surfaces a
// wrapped fs.ErrNotExist error suggesting `j init`; a missing or
// unparseable value surfaces the matching sentinel above.
func LoadConfig() (Config, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return Config{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, fmt.Errorf("workflow: settings store missing at %q; run `j init`: %w", path, err)
		}
		return Config{}, fmt.Errorf("workflow: stat %q: %w", path, err)
	}

	s, err := store.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("workflow: open settings: %w", err)
	}
	defer s.Close()

	apiKey, err := readSetting(s, "api_key")
	if err != nil {
		return Config{}, err
	}
	if apiKey == "" {
		return Config{}, ErrMissingAPIKey
	}
	model, err := readSetting(s, "model")
	if err != nil {
		return Config{}, err
	}
	if model == "" {
		return Config{}, ErrMissingModel
	}
	rawIters, err := readSetting(s, "max_iterations")
	if err != nil {
		return Config{}, err
	}
	if rawIters == "" {
		return Config{}, ErrMissingMaxIterations
	}
	n, err := strconv.ParseUint(rawIters, 10, 64)
	if err != nil || n == 0 {
		return Config{}, ErrMissingMaxIterations
	}

	return Config{APIKey: apiKey, Model: model, MaxIterations: uint(n)}, nil
}

// readSetting reads a single key from the project bucket. Whitespace
// is trimmed so a stray space stored via `j settings set` doesn't
// silently defeat the missing-value check.
func readSetting(s *store.Store, key string) (string, error) {
	v, _, err := s.Get(store.BucketProject, key)
	if err != nil {
		return "", fmt.Errorf("workflow: read project.%s: %w", key, err)
	}
	return strings.TrimSpace(v), nil
}
