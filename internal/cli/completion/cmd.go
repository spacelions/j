// Package completion implements the `j completion` subcommand.
package completion

import (
	"github.com/spf13/cobra"
)

// New returns the completion cobra command tree (bash/zsh/fish/powershell).
func New(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for j.

Bash:
  echo 'source <(j completion bash)' >> ~/.bashrc

Zsh:
  j completion zsh > "${fpath[1]}/_j"

Fish:
  j completion fish | source

PowerShell:
  j completion powershell | Out-String | Invoke-Expression`,
		Args:               cobra.ExactArgs(1),
		ValidArgs:          []string{"bash", "zsh", "fish", "powershell"},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return cmd.Usage()
			}
		},
	}
	return cmd
}
