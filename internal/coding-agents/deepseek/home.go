package deepseek

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	envHome    = "DEEPSEEK_HOME"
	homeSubdir = ".deepseek-home"
)

func defaultHome() string {
	if home := os.Getenv(envHome); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".deepseek")
}

func prepareScopedEnv(taskDir, resumeID string) ([]string, error) {
	home, err := populateScopedHome(taskDir)
	if err != nil {
		return nil, err
	}
	if err := migrateLegacySession(taskDir, resumeID); err != nil {
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

func migrateLegacySession(taskDir, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	legacySessions := filepath.Join(defaultHome(), "sessions")
	entries, err := os.ReadDir(legacySessions)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		src := filepath.Join(legacySessions, entry.Name())
		meta, ok := decodeSession(src)
		if !ok || meta.ID != sessionID {
			continue
		}
		dst := filepath.Join(sessionsDir(taskDir), entry.Name())
		return symlinkIfMissing(src, dst)
	}
	return nil
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
