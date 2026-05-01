// Package uitheme exposes the dark-terminal huh theme used by every
// huh.NewForm call site in j. Centralising the choice in a single
// helper means every prompt -- preflight, init, tasks, plan, work --
// renders with the same readable palette on a dark background and a
// future palette swap is one line away.
package uitheme

import "github.com/charmbracelet/huh"

// Theme returns the dark-terminal huh theme used by every huh.NewForm
// call site in j. Callers do not have to know which concrete palette
// is in use; bumping the palette is a single-line change here.
func Theme() *huh.Theme { return huh.ThemeCatppuccin() }
