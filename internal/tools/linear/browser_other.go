//go:build !darwin

package linear

// browserBin is the platform binary openURL shells out to for
// "launch the default browser at this URL". Non-macOS systems use
// the freedesktop.org `xdg-open` helper.
const browserBin = "xdg-open"
