package completion

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/testutil"
)

func TestNew_GeneratesShellCompletion(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			root := &cobra.Command{Use: "j"}
			cmd := New(root)

			stdout, _, err := testutil.RunCobra(t, cmd, shell)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !strings.Contains(stdout, "j") {
				t.Fatalf("stdout = %q, want completion script", stdout)
			}
		})
	}
}

func TestNew_UnknownShellShowsUsage(t *testing.T) {
	cmd := New(&cobra.Command{Use: "j"})

	stdout, stderr, err := testutil.RunCobra(t, cmd, "unknown")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := stdout + stderr
	if !strings.Contains(out, "completion [bash|zsh|fish|powershell]") {
		t.Fatalf("output = %q, want usage", out)
	}
}

func TestNew_RequiresShell(t *testing.T) {
	cmd := New(&cobra.Command{Use: "j"})

	_, _, err := testutil.RunCobra(t, cmd)
	if err == nil {
		t.Fatal("Execute succeeded, want argument error")
	}
}
