package testcases_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNoWindowsBuildTagsInSource verifies AC13: the project has
// dropped all Windows-specific build guards. Scans every .go file
// under internal/ and testcases/ for the platform-specific patterns
// that must not appear (see forbidden slice below).
func TestNoWindowsBuildTagsInSource(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	testcasesDir := filepath.Dir(thisFile)
	projectRoot := filepath.Dir(testcasesDir)

	// Patterns are split across concatenations so this file itself
	// does not contain the literal substrings it is checking for.
	w := "windows"
	forbidden := []string{
		"go:build" + " " + w,
		"go:build" + " !" + w,
		"runtime.GOOS" + ` == "` + w + `"`,
	}
	scan := func(dir string) {
		err := filepath.WalkDir(
			dir, func(
				path string, d fs.DirEntry, walkErr error,
			) error {
				if walkErr != nil {
					return walkErr
				}
				// Skip this file: it contains the patterns as
				// split literals and would false-positive.
				if path == thisFile {
					return nil
				}
				if d.IsDir() ||
					!strings.HasSuffix(path, ".go") {
					return nil
				}
				data, err := os.ReadFile(path) //nolint:gosec // path is from WalkDir over known dirs
				if err != nil {
					return err
				}
				content := string(data)
				for _, pat := range forbidden {
					if strings.Contains(content, pat) {
						t.Errorf(
							"AC13: %q contains %q",
							path, pat,
						)
					}
				}
				return nil
			},
		)
		if err != nil {
			t.Fatalf("WalkDir %s: %v", dir, err)
		}
	}
	scan(filepath.Join(projectRoot, "internal"))
	scan(testcasesDir)
}
