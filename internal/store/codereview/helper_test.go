package codereview

import "os"

// writeFile is a thin os.WriteFile wrapper used by review_test.go to
// drop pre-canned TOML bodies without re-declaring the mode constant.
func writeFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o644)
}

// chmod is a thin os.Chmod wrapper exposed to tests that exercise
// the Save error paths.
func chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}
