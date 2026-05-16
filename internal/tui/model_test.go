package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestQuitCommandExits(t *testing.T) {
	m := NewModel()

	// Send window size to initialize viewport.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	// Press "/" to enter command mode.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)

	// Type "quit".
	for _, r := range "quit" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
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
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	// Ctrl+C with empty input in normal mode.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)

	if !strings.Contains(strings.ToLower(m.StatusMsg()), "quit") {
		t.Errorf("expected hint about /quit in status bar, got: %q", m.StatusMsg())
	}
}

func TestSlashOpensCommandMode(t *testing.T) {
	m := NewModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	if m.mode != ModeNormal {
		t.Fatal("expected ModeNormal initially")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)

	if m.mode != ModeCommand {
		t.Errorf("expected ModeCommand after pressing '/', got %v", m.mode)
	}
}

func TestEscCancelsCommand(t *testing.T) {
	m := NewModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	// Enter command mode.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)

	if m.mode != ModeCommand {
		t.Fatal("expected ModeCommand after pressing '/'")
	}

	// Press Esc to cancel.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.mode != ModeNormal {
		t.Errorf("expected ModeNormal after Esc, got %v", m.mode)
	}

	if m.input.Value() != "" {
		t.Errorf("expected empty input after Esc, got %q", m.input.Value())
	}
}

func TestCtrlCClearsInput(t *testing.T) {
	m := NewModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	// Enter command mode and type some text.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)

	for _, r := range "send" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}

	// Ctrl+C should clear input and return to normal mode.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)

	if m.mode != ModeNormal {
		t.Errorf("expected ModeNormal after Ctrl+C with text, got %v", m.mode)
	}

	if m.input.Value() != "" {
		t.Errorf("expected empty input after Ctrl+C, got %q", m.input.Value())
	}
}
