package testcases_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestLefthook_PreCommit_ChainOrder pins the acceptance criterion that
// the pre-commit chain runs in the order line-cap → lint → tests, with
// distinct ascending priorities so lefthook executes them in that order.
func TestLefthook_PreCommit_ChainOrder(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(thisFile))
	path := filepath.Join(repoRoot, "lefthook.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	body := string(raw)

	wantOrder := []string{"line-cap", "lint", "tests"}
	idx := make(map[string]int, len(wantOrder))
	for _, name := range wantOrder {
		re := regexp.MustCompile(`(?m)^\s{4}` + regexp.QuoteMeta(name) +
			`:\s*$`)
		loc := re.FindStringIndex(body)
		if loc == nil {
			t.Fatalf("lefthook.yml: missing pre-commit command %q",
				name)
		}
		idx[name] = loc[0]
	}
	if idx["line-cap"] >= idx["lint"] || idx["lint"] >= idx["tests"] {
		t.Fatalf("lefthook.yml: command order should be line-cap, "+
			"lint, tests; got positions %v", idx)
	}

	// Each command must declare a priority. Extract them and verify
	// they are strictly ascending in the same order.
	prios := map[string]int{}
	for _, name := range wantOrder {
		re := regexp.MustCompile(`(?m)^\s{4}` + regexp.QuoteMeta(name) +
			`:\s*\n(?:\s{6}.+\n)*?\s{6}priority:\s*(\d+)\s*$`)
		m := re.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("lefthook.yml: %q is missing a priority field",
				name)
		}
		// Parse digits manually to avoid extra deps.
		n := 0
		for _, c := range m[1] {
			n = n*10 + int(c-'0')
		}
		prios[name] = n
	}
	if prios["line-cap"] >= prios["lint"] ||
		prios["lint"] >= prios["tests"] {
		t.Fatalf("lefthook.yml: priorities must be strictly ascending "+
			"line-cap < lint < tests; got %v", prios)
	}

	// Sanity: the priority field for `lint` should sit between the two
	// other priorities (no shared/duplicate values).
	all := []int{prios["line-cap"], prios["lint"], prios["tests"]}
	seen := map[int]bool{}
	for _, p := range all {
		if seen[p] {
			t.Fatalf("lefthook.yml: duplicate priority value %d "+
				"across commands %v", p, prios)
		}
		seen[p] = true
	}

	// The header comment block should still describe all three steps so
	// the file remains self-documenting.
	for _, hint := range []string{"line-cap", "lint", "tests"} {
		if !strings.Contains(body, hint) {
			t.Fatalf("lefthook.yml: header/comments must mention %q",
				hint)
		}
	}
}
