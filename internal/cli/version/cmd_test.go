package version

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/testutil"
)

func TestNew_PrintsVersion(t *testing.T) {
	old := Version
	Version = "v1.2.3-test"
	t.Cleanup(func() { Version = old })

	stdout, _, err := testutil.RunCobra(t, New())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := strings.TrimSpace(stdout); got != "v1.2.3-test" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestNew_RejectsArgs(t *testing.T) {
	_, _, err := testutil.RunCobra(t, New(), "extra")
	if err == nil {
		t.Fatal("Execute succeeded, want argument error")
	}
}
