package linear

import (
	"fmt"
	"os/exec"
)

// OpenURL is the package-level hook used by the source picker to open
// a browser tab on the Linear API-keys page during the link prompt.
// The default implementation shells out to the platform's standard
// "open the default app for this URL" command (`open` on macOS,
// `xdg-open` elsewhere). Tests overwrite the var to assert the prompt
// fires without launching a real browser; this is a deliberate
// AGENTS.md "allowlist" rather than a behaviour-bearing seam —
// production code never reads it back.
var OpenURL = openURL

func openURL(url string) error {
	cmd := exec.Command(browserBin, url)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("linear: open url: %w", err)
	}
	return nil
}
