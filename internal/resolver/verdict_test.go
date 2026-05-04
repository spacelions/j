package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/store"
)

func TestParseVerdict(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body string
		want string
	}{
		{"pass", "notes\n  VERDICT: pass\n", "PASS"},
		{"fail", "VERDICT: FAIL\r\n", "FAIL"},
		{"malformed", "VERDICT: MAYBE\n", "FAIL"},
		{"empty", "\n\n", "FAIL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".md")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := ParseVerdict(path); got != tc.want {
				t.Fatalf("ParseVerdict = %q, want %q", got, tc.want)
			}
		})
	}
	if got := ParseVerdict(filepath.Join(dir, "missing.md")); got != "FAIL" {
		t.Fatalf("missing verdict = %q", got)
	}
}

func TestReadVerdictForTask(t *testing.T) {
	setupResolverProject(t)
	dir, err := store.EnsureTaskDir("task")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, store.VerifierFindingsFileName), []byte("VERDICT: PASS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadVerdictForTask("task"); got != "PASS" {
		t.Fatalf("ReadVerdictForTask = %q", got)
	}
}
