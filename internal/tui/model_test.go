package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
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

func TestLayoutHeightFitsWindow(t *testing.T) {
	m := NewModel()
	const width, height = 100, 30
	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height}); return updated }())

	// Layout: header(1) + divider(1) + feed(height-6) + divider(1) + statusbar(1) + input(1) + divider(1) = height.
	wantFeedHeight := height - 6
	if m.viewport.Height != wantFeedHeight {
		t.Errorf("expected viewport height %d, got %d", wantFeedHeight, m.viewport.Height)
	}
	if m.viewport.Width != width {
		t.Errorf("expected viewport width %d, got %d", width, m.viewport.Width)
	}

	// Resize to a different size: the else-branch must update both dimensions.
	const width2, height2 = 120, 40
	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.WindowSizeMsg{Width: width2, Height: height2}); return updated }())

	wantFeedHeight2 := height2 - 6
	if m.viewport.Height != wantFeedHeight2 {
		t.Errorf("after resize: expected viewport height %d, got %d", wantFeedHeight2, m.viewport.Height)
	}
	if m.viewport.Width != width2 {
		t.Errorf("after resize: expected viewport width %d, got %d", width2, m.viewport.Width)
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

func TestFeedRenderReply(t *testing.T) {
	now := time.Now()
	createAt := now.UnixMilli()

	msgCache := map[string]string{
		"parent-id": "Hello everyone, how are you doing today?",
	}

	post := mattermost.Message{
		ID:        "reply-id",
		ChannelID: "ch1",
		UserID:    "user1",
		Text:      "I am fine, thanks!",
		CreateAt:  createAt,
		RootID:    "parent-id",
	}

	line := renderMessageLine(post, "alice", "general", msgCache, 120)

	if !strings.Contains(line, "↩") {
		t.Errorf("expected thread reply indicator ↩ in line, got: %q", line)
	}
	if !strings.Contains(line, "alice") {
		t.Errorf("expected sender name in line, got: %q", line)
	}
	if !strings.Contains(line, "I am fine, thanks!") {
		t.Errorf("expected message text in line, got: %q", line)
	}
	if !strings.Contains(line, "Hello everyone") {
		t.Errorf("expected parent snippet in line, got: %q", line)
	}
}

func TestFeedRenderReplyNoParent(t *testing.T) {
	createAt := time.Now().UnixMilli()
	msgCache := map[string]string{}

	post := mattermost.Message{
		ID:       "reply-id",
		RootID:   "unknown-parent",
		Text:     "my reply",
		CreateAt: createAt,
	}

	line := renderMessageLine(post, "bob", "general", msgCache, 120)

	if !strings.Contains(line, "↩") {
		t.Errorf("expected ↩ indicator even without parent snippet, got: %q", line)
	}
	if !strings.Contains(line, "my reply") {
		t.Errorf("expected message text in line, got: %q", line)
	}
	// No parent snippet when not in cache.
	if strings.Contains(line, "В ответ на") {
		t.Errorf("expected no parent snippet when parent not in cache, got: %q", line)
	}
}

func TestFeedRenderNormalMessage(t *testing.T) {
	createAt := time.Now().UnixMilli()
	msgCache := map[string]string{}

	post := mattermost.Message{
		ID:       "msg-id",
		Text:     "hello world",
		CreateAt: createAt,
	}

	line := renderMessageLine(post, "charlie", "random", msgCache, 120)

	if strings.Contains(line, "↩") {
		t.Errorf("expected no thread indicator for top-level message, got: %q", line)
	}
	if !strings.Contains(line, "charlie") {
		t.Errorf("expected sender name in line, got: %q", line)
	}
	if !strings.Contains(line, "hello world") {
		t.Errorf("expected message text in line, got: %q", line)
	}
	if !strings.Contains(line, "#random") {
		t.Errorf("expected channel name in line, got: %q", line)
	}
}

func TestFeedRenderWordWrap(t *testing.T) {
	createAt := time.Now().UnixMilli()
	msgCache := map[string]string{}

	longText := strings.Repeat("word ", 30) // 150 chars — will need wrapping at width=40
	post := mattermost.Message{
		ID:       "msg-id",
		Text:     longText,
		CreateAt: createAt,
	}

	line := renderMessageLine(post, "dave", "chan", msgCache, 40)

	lines := strings.Split(line, "\n")
	// header line + up to 3 body lines + optional ⌄⌄⌄ = at most 5 lines
	if len(lines) > 5 {
		t.Errorf("expected at most 5 lines (header + 3 body + overflow), got %d", len(lines))
	}
	// last line should contain the overflow indicator since there are many words
	if !strings.Contains(lines[len(lines)-1], "⌄⌄⌄") {
		t.Errorf("expected overflow indicator ⌄⌄⌄ in last line, got: %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[len(lines)-1], "more lines") {
		t.Errorf("expected 'more lines' text in overflow indicator, got: %q", lines[len(lines)-1])
	}
}
