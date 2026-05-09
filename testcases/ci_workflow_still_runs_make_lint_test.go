package testcases_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestCI_Workflow_StillRunsMakeLint pins the acceptance criterion that
// the GitHub CI workflow continues to run lint at least once per
// push/PR. The hook addition is a local-developer convenience; CI
// coverage must not regress.
func TestCI_Workflow_StillRunsMakeLint(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(thisFile))
	path := filepath.Join(repoRoot, ".github", "workflows", "ci.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	body := string(raw)

	// Trigger must include `push` and `pull_request` so lint runs on
	// both events that gate merges.
	for _, evt := range []string{"push", "pull_request"} {
		evtRe := regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(evt) +
			`:\s*$`)
		if !evtRe.MatchString(body) {
			t.Fatalf("ci.yml: missing workflow trigger %q", evt)
		}
	}

	for _, cmd := range []string{
		"make lint",
		"make test",
		"make e2e",
		"make line-coverage",
	} {
		run := regexp.MustCompile(`(?m)^\s+run:\s*` +
			regexp.QuoteMeta(cmd) + `\s*$`)
		if !run.MatchString(body) {
			t.Fatalf("ci.yml: no step runs `%s`; CI coverage "+
				"would regress", cmd)
		}
	}

	if strings.Contains(body, "make branch-coverage") {
		t.Fatalf("ci.yml: must not run `make branch-coverage`")
	}
}
