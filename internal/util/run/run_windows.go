//go:build windows

package run

import "os/exec"

// applyDetachAttrs is a no-op on Windows: the cursor-agent backend's
// background path is POSIX-only by design (see internal/coding-agents/
// cursor "build !windows" tests) so there is no detach attribute to
// apply here. The helper still exists so platform-agnostic code can
// call it unconditionally.
func applyDetachAttrs(_ *exec.Cmd) {}
