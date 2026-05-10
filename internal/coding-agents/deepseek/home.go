package deepseek

import (
	"os"
	"path/filepath"
)

const (
	envHome    = "DEEPSEEK_HOME"
	homeSubdir = ".deepseek-home"
)

func scopedEnv(taskDir string) ([]string, error) {
	home, err := scopedHome(taskDir)
	if err != nil {
		return nil, err
	}
	return []string{envHome + "=" + home}, nil
}

func scopedHome(taskDir string) (string, error) {
	home := filepath.Join(taskDir, homeSubdir)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	return home, nil
}

func sessionsDir(taskDir string) string {
	return filepath.Join(taskDir, homeSubdir, "sessions")
}
