package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func mustModel(t *testing.T, m tea.Model) Model {
	t.Helper()
	typed, ok := m.(Model)
	if !ok {
		t.Fatalf("expected tui.Model, got %T", m)
	}
	return typed
}

func TestQuitCommandExits(t *testing.T) {
	m := NewModel()

	// Send window size to initialize viewport.
	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); return updated }())

	// Press "/" to enter command mode.
	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}); return updated }())

	// Type "quit".
	for _, r := range "quit" {
		r := r
		m = mustModel(t, func() tea.Model {
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			return updated
		}())
	}

	// Press Enter to execute the command.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected quit cmd, got nil")
	}

	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestCtrlCEmptyFieldShowsHint(t *testing.T) {
	m := NewModel()

	// Send window size to initialize viewport.
	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); return updated }())

	// Ctrl+C with empty input in normal mode.
	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); return updated }())

	if !strings.Contains(strings.ToLower(m.StatusMsg()), "quit") {
		t.Errorf("expected hint about /quit in status bar, got: %q", m.StatusMsg())
	}
}

func TestSlashOpensCommandMode(t *testing.T) {
	m := NewModel()

	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); return updated }())

	if m.mode != ModeNormal {
		t.Fatal("expected ModeNormal initially")
	}

	m = mustModel(t, func() tea.Model {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		return updated
	}())

	if m.mode != ModeCommand {
		t.Errorf("expected ModeCommand after pressing '/', got %v", m.mode)
	}
}

func TestEscCancelsCommand(t *testing.T) {
	m := NewModel()

	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); return updated }())

	// Enter command mode.
	m = mustModel(t, func() tea.Model {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		return updated
	}())

	if m.mode != ModeCommand {
		t.Fatal("expected ModeCommand after pressing '/'")
	}

	// Press Esc to cancel.
	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc}); return updated }())

	if m.mode != ModeNormal {
		t.Errorf("expected ModeNormal after Esc, got %v", m.mode)
	}

	if m.input.Value() != "" {
		t.Errorf("expected empty input after Esc, got %q", m.input.Value())
	}
}

func TestCtrlCClearsInput(t *testing.T) {
	m := NewModel()

	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); return updated }())

	// Enter command mode and type some text.
	m = mustModel(t, func() tea.Model {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		return updated
	}())

	for _, r := range "send" {
		r := r
		m = mustModel(t, func() tea.Model {
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			return updated
		}())
	}

	// Ctrl+C should clear input and return to normal mode.
	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); return updated }())

	if m.mode != ModeNormal {
		t.Errorf("expected ModeNormal after Ctrl+C with text, got %v", m.mode)
	}

	if m.input.Value() != "" {
		t.Errorf("expected empty input after Ctrl+C, got %q", m.input.Value())
	}
}
