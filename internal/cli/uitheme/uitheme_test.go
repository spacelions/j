package uitheme

import (
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// TestThemeNonNil pins the contract Theme() must always satisfy:
// it returns a usable *huh.Theme so callers can wire it into a
// huh.NewForm chain without a nil check at every site.
func TestThemeNonNil(t *testing.T) {
	if Theme() == nil {
		t.Fatal("Theme() returned nil")
	}
}

// TestThemeOverridesBase locks the regression that motivated the
// helper. A future edit that drops the dark-palette override and
// falls back to the base theme would leave Focused.Title with no
// foreground colour. Comparing the foreground against the zero
// lipgloss.NoColor{} returned by the base theme catches that
// without pinning to a specific palette identity.
func TestThemeOverridesBase(t *testing.T) {
	got := Theme().Focused.Title.GetForeground()
	if got == (lipgloss.NoColor{}) {
		t.Fatalf("Theme().Focused.Title foreground = %#v, want non-zero override of base theme", got)
	}
	base := huh.ThemeBase().Focused.Title.GetForeground()
	if got == base {
		t.Fatalf("Theme().Focused.Title foreground = %#v matches base theme; expected override", got)
	}
}
