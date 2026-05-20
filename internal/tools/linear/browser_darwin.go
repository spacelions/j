//go:build darwin

package linear

// browserBin is the platform binary openURL shells out to for
// "launch the default browser at this URL". macOS exposes this via
// the standard `open` command.
const browserBin = "open"
