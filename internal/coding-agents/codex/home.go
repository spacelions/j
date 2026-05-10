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

func prepareScopedEnv(taskDir string) ([]string, error) {
	home, err := populateScopedHome(taskDir)
	if err != nil {
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
		if err := symlinkToTarget(src, dst); err != nil {
			return "", err
		}
	}
	return home, nil
}

func defaultHome() string {
	if home := os.Getenv(envHome); home != "" {
		abs, _ := filepath.Abs(home)
		return abs
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func symlinkToTarget(src, dst string) error {
	src, _ = filepath.Abs(src)
	current, err := os.Readlink(dst)
	if err == nil {
		if sameSymlinkTarget(current, src, dst) {
			return nil
		}
		if err := os.Remove(dst); err != nil {
			return err
		}
		return createSymlink(src, dst)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return createSymlink(src, dst)
	}
	if info, statErr := os.Lstat(dst); statErr == nil &&
		info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	return err
}

func createSymlink(src, dst string) error {
	return os.Symlink(src, dst)
}

func sameSymlinkTarget(target, src, link string) bool {
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	target, _ = filepath.Abs(target)
	return filepath.Clean(target) == filepath.Clean(src)
}

func sessionsDir(taskDir string) string {
	return filepath.Join(taskDir, homeSubdir, "sessions")
}
