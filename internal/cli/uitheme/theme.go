// Package uitheme centralises CLI terminal presentation: the dark-terminal
// huh theme used by every huh.NewForm call site, Normal/Dangerous dialog_box
// helpers for stdout/stderr messaging and fork banners, the bordered task
// table renderer for `j tasks`, and the bubbletea watch loop that refreshes
// that table every second.
package uitheme

import "github.com/charmbracelet/huh"

// Theme returns the dark-terminal huh theme used by every huh.NewForm
// call site in j. Callers do not have to know which concrete palette
// is in use; bumping the palette is a single-line change here.
func Theme() *huh.Theme { return huh.ThemeCatppuccin() }
