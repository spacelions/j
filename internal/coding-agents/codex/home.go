package codex

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	envHome    = "CODEX_HOME"
	homeSubdir = ".codex-home"
)

func defaultHome() string {
	if home := os.Getenv(envHome); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func prepareScopedEnv(taskDir, resumeID string) ([]string, error) {
	home, err := populateScopedHome(taskDir)
	if err != nil {
		return nil, err
	}
	if err := migrateLegacyRollout(taskDir, resumeID); err != nil {
		return nil, err
	}
	return []string{envHome + "=" + home}, nil
}

func populateScopedHome(taskDir string) (string, error) {
	home := filepath.Join(taskDir, homeSubdir)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(sessionsDir(taskDir), 0o700); err != nil {
		return "", err
	}
	realHome := defaultHome()
	entries, err := os.ReadDir(realHome)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return home, nil
		}
		return "", err
	}
	for _, entry := range entries {
		if entry.Name() == "sessions" {
			continue
		}
		src := filepath.Join(realHome, entry.Name())
		dst := filepath.Join(home, entry.Name())
		if err := symlinkIfMissing(src, dst); err != nil {
			return "", err
		}
	}
	return home, nil
}

func migrateLegacyRollout(taskDir, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	legacySessions := filepath.Join(defaultHome(), "sessions")
	if _, err := os.Stat(legacySessions); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	scopedSessions := sessionsDir(taskDir)
	return filepath.WalkDir(legacySessions, func(
		path string, entry fs.DirEntry, err error,
	) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !isRollout(entry.Name()) {
			return nil
		}
		meta, ok := decodeMeta(path)
		if !ok || meta.ID != sessionID {
			return nil
		}
		rel, _ := filepath.Rel(legacySessions, path)
		target := filepath.Join(scopedSessions, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := symlinkIfMissing(path, target); err != nil {
			return err
		}
		return filepath.SkipAll
	})
}

func symlinkIfMissing(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Symlink(src, dst); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	return nil
}

func sessionsDir(taskDir string) string {
	return filepath.Join(taskDir, homeSubdir, "sessions")
}
