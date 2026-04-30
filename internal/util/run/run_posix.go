//go:build !windows

package run

import (
	"os/exec"
	"syscall"
)

// applyDetachAttrs configures cmd so the spawned child runs in a
// fresh POSIX session (setsid). Detaching from the parent's
// controlling terminal lets the child survive SIGHUP / terminal
// close, which is the equivalent of `nohup` for fire-and-forget
// background runs of cursor-agent.
func applyDetachAttrs(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
