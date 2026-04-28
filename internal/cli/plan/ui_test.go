package plan

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSelectModel_NavigateAndSubmit(t *testing.T) {
	m := newSelectModel("Pick", []string{"a", "b", "c"})

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyDown})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := nm.(selectModel).cursor; got != 2 {
		t.Fatalf("cursor = %d, want clamp at 2", got)
	}
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyUp})
	nm, cmd := nm.(selectModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	sm := nm.(selectModel)
	if !sm.submitted {
		t.Fatal("not submitted")
	}
	if sm.cancelled {
		t.Fatal("should not be cancelled")
	}
	if sm.options[sm.cursor] != "b" {
		t.Fatalf("selected = %q, want %q", sm.options[sm.cursor], "b")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd")
	}
}

func TestSelectModel_VimKeys(t *testing.T) {
	m := newSelectModel("Pick", []string{"a", "b"})
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if c := nm.(selectModel).cursor; c != 1 {
		t.Fatalf("after j: cursor = %d", c)
	}
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if c := nm.(selectModel).cursor; c != 0 {
		t.Fatalf("after k: cursor = %d", c)
	}
}

func TestSelectModel_Cancel(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyEsc},
	} {
		m := newSelectModel("Pick", []string{"a", "b"})
		nm, _ := m.Update(key)
		if !nm.(selectModel).cancelled {
			t.Fatalf("expected cancel on %v", key)
		}
	}
}

func TestSelectModel_View(t *testing.T) {
	m := newSelectModel("Pick", []string{"a", "b"})
	v := m.View()
	if !strings.Contains(v, "Pick") {
		t.Fatalf("view missing title: %q", v)
	}
	if !strings.Contains(v, "> a") {
		t.Fatalf("view missing cursor marker: %q", v)
	}
}

func TestTargetModel_SubmitTrim(t *testing.T) {
	m := newTargetModel()
	m.input.SetValue("  /tmp/x.md  ")
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tm := nm.(targetModel)
	if !tm.submitted {
		t.Fatal("not submitted")
	}
	if tm.value != "/tmp/x.md" {
		t.Fatalf("value = %q", tm.value)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd")
	}
}

func TestTargetModel_Cancel(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyEsc},
	} {
		m := newTargetModel()
		nm, _ := m.Update(key)
		if !nm.(targetModel).cancelled {
			t.Fatalf("expected cancel on %v", key)
		}
	}
}

func TestTargetModel_TypingPassesThrough(t *testing.T) {
	m := newTargetModel()
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if got := nm.(targetModel).input.Value(); got == "" {
		t.Fatal("expected typed runes to populate the textinput")
	}
}

func TestTargetModel_View(t *testing.T) {
	m := newTargetModel()
	if !strings.Contains(m.View(), "Markdown file location") {
		t.Fatalf("view missing prompt: %q", m.View())
	}
}

func TestTargetModel_Init(t *testing.T) {
	if newTargetModel().Init() == nil {
		t.Fatal("Init should return textinput.Blink")
	}
}

func TestSelectModel_Init(t *testing.T) {
	if newSelectModel("t", []string{"a"}).Init() != nil {
		t.Fatal("Init should be nil")
	}
}
