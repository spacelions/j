package uitheme

import (
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

func TestThemeNonNil(t *testing.T) {
	if Theme() == nil {
		t.Fatal("Theme() returned nil")
	}
}

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
