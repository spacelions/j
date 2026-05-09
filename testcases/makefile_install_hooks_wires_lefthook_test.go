package testcases_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// TestMakefile_InstallHooks_WiresLefthook pins the acceptance criterion
// that `make install-hooks` wires the updated chain without any extra
// manual steps. Concretely, the target must invoke
// `go tool lefthook install` so a fresh clone followed by
// `make install-hooks` enables the new lint step automatically.
func TestMakefile_InstallHooks_WiresLefthook(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(thisFile))
	path := filepath.Join(repoRoot, "Makefile")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	body := string(raw)

	// install-hooks target must exist and the recipe must run
	// `go tool lefthook install`. The recipe is allowed any flags.
	target := regexp.MustCompile(`(?m)^install-hooks:\s*$`)
	if !target.MatchString(body) {
		t.Fatalf("Makefile: missing `install-hooks:` target")
	}
	wire := regexp.MustCompile(
		`(?m)^install-hooks:\s*\n(?:\t.*\n)*?\t.*go tool lefthook install`)
	if !wire.MatchString(body) {
		t.Fatalf("Makefile: install-hooks must run " +
			"`go tool lefthook install` so the new lint step gets " +
			"wired by a fresh clone + make install-hooks")
	}
}
