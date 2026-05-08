// Package task owns the per-project task storage. Each task is a
// directory under `<cwd>/.j/tasks/<id>/` holding the row metadata
// (`task.toml`), the requirement and plan markdown the agent
// produces, and the `agent.log` capture of any background spawn.
// One file per task means concurrent writers to different IDs never
// contend, and atomic write+rename guarantees readers see either
// the old row or the new one — never a partial write.
package tasks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/store"
)

// DirName is the per-project tasks directory inside the `.j` folder.
// `<cwd>/.j/<DirName>/` holds one subdirectory per task.
const DirName = "tasks"

// PlanFileName is the filename of the plan markdown stored under
// `<cwd>/.j/tasks/<id>/`. `j plan` writes it; `j work` reads it.
const PlanFileName = "plan.md"

// RequirementsFileName is the filename of the requirements markdown
// stored under `<cwd>/.j/tasks/<id>/`. `j plan` writes it; `j work`
// and `j tasks` summary derivation read it.
const RequirementsFileName = "requirements.md"

// ClarificationFileName is the filename agents write when they need
// human clarification before proceeding. Its presence in the per-task
// directory after the agent exits cleanly signals
// `needs-clarification`; the planner contract for it lives in
// `internal/agents/instructions/planner.md`.
const ClarificationFileName = "clarification.md"

// Store is the task-package handle for the per-project tasks tree
// (`<cwd>/.j/tasks/`). Construct one with Open and call Close when
// done. Close is a no-op (no file descriptors are held); the field is
// preserved for symmetry with store.Store and so callers can still
// `defer s.Close()`. The zero value is not usable.
type Store struct {
	tasksDir string
}

// Open returns a Store rooted at tasksDir (typically
// `<cwd>/.j/tasks`). Unlike store.Open, this never returns an error
// — there is no file lock to acquire and no bucket to validate; the
// per-method file ops surface failures (e.g. fs.ErrNotExist when the
// dir is missing) instead.
func Open(tasksDir string) *Store {
	return &Store{tasksDir: tasksDir}
}

// OpenDefault is the common shorthand for DefaultDir + Open. The
// returned Store is rooted at `<cwd>/.j/tasks`; the only failure mode
// is DefaultDir's (missing cwd / store root). Per-method file ops
// surface fs.ErrNotExist when the dir itself is missing — callers
// that care about that case should branch on those errors rather
// than stat-checking the dir up front.
func OpenDefault() (*Store, error) {
	dir, err := DefaultDir()
	if err != nil {
		return nil, err
	}
	return Open(dir), nil
}

// Dir returns the absolute path the Store is rooted at. Useful for
// callers that need to construct per-task paths (e.g.
// `<dir>/<id>/agent.log`) without re-deriving the root.
func (s *Store) Dir() string { return s.tasksDir }

// Close releases any per-Store resources. Files are opened/closed
// inside each method so this is a no-op today; it stays in the API
// for symmetry with store.Store and so callers can pair Open with a
// `defer s.Close()` without thinking about it.
func (s *Store) Close() error { return nil }

// DefaultDir returns the absolute path to the per-project tasks
// directory (`<cwd>/.j/tasks`). The directory holds one subdirectory
// per task; each holds `requirements.md`, `plan.md`, `agent.log`,
// and `task.toml` (the row metadata).
func DefaultDir() (string, error) {
	dir, err := store.DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DirName), nil
}

// EnsureDir creates `<cwd>/.j/tasks/<id>/` (with mkdir -p) and
// returns its absolute path. The parent `.j/tasks/` directory must
// already exist (created by `j init` via store.store.EnsureProject); a
// missing parent surfaces a wrapped fs.ErrNotExist so callers can
// prompt the user to run init.
func EnsureDir(id string) (string, error) {
	if id == "" {
		return "", errors.New("task: empty task id")
	}
	tasksDir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(tasksDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("task: %q missing; run `j init`: %w", tasksDir, err)
		}
		return "", fmt.Errorf("task: stat %q: %w", tasksDir, err)
	}
	taskDir := filepath.Join(tasksDir, id)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return "", fmt.Errorf("task: mkdir %q: %w", taskDir, err)
	}
	return taskDir, nil
}

// RemoveDir removes `<cwd>/.j/tasks/<id>/` and every artifact inside
// it. The parent `.j/tasks/` directory must already exist (created by
// `j init` via store.store.EnsureProject); a missing parent surfaces a
// wrapped fs.ErrNotExist so callers can prompt the user to run init
// (mirroring EnsureDir). The helper is idempotent: a missing per-task
// directory is treated as a no-op because os.RemoveAll returns nil
// when the target is absent.
func RemoveDir(id string) error {
	if id == "" {
		return errors.New("task: empty task id")
	}
	tasksDir, err := DefaultDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(tasksDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("task: %q missing; run `j init`: %w", tasksDir, err)
		}
		return fmt.Errorf("task: stat %q: %w", tasksDir, err)
	}
	taskDir := filepath.Join(tasksDir, id)
	if err := os.RemoveAll(taskDir); err != nil {
		return fmt.Errorf("task: remove %q: %w", taskDir, err)
	}
	return nil
}
