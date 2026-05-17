package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
)

// TestCtrlLActivatesChannelsMode verifies that Ctrl+L switches to ModeChannels.
func TestCtrlLActivatesChannelsMode(t *testing.T) {
	m := initModel(t, NewModel())
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlL})
	if m.mode != ModeChannels {
		t.Errorf("expected ModeChannels after Ctrl+L, got %v", m.mode)
	}
}

// TestEscFromChannelsGoesToInput verifies that Esc from ModeChannels returns to ModeInput.
func TestEscFromChannelsGoesToInput(t *testing.T) {
	m := initModel(t, NewModel())
	m.mode = ModeChannels
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != ModeInput {
		t.Errorf("expected ModeInput after Esc from ModeChannels, got %v", m.mode)
	}
}

// TestChannelsViewArrowSelectsOnly verifies that arrow keys in ModeChannels move the cursor
// but do not change the open channel.
func TestChannelsViewArrowSelectsOnly(t *testing.T) {
	m := initModel(t, NewModel())
	m.channelsView = NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
		{ID: "ch2", Name: "backend"},
	})
	m.channelsView = m.channelsView.SetSize(22, 20)
	m.mode = ModeChannels
	initialOpenIdx := m.channelsView.openIdx

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyDown})

	if m.channelsView.openIdx != initialOpenIdx {
		t.Errorf("arrow key should not change openIdx: before=%d after=%d", initialOpenIdx, m.channelsView.openIdx)
	}
	if m.channelsView.selectedIdx == 0 {
		t.Error("expected selectedIdx to change after Down key")
	}
}

// TestChannelsViewEnterOpensChannel verifies that Enter in ModeChannels emits MsgChannelSelected.
func TestChannelsViewEnterOpensChannel(t *testing.T) {
	m := initModel(t, NewModel())
	m.channelsView = NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
	})
	m.channelsView = m.channelsView.SetSize(22, 20)
	m.mode = ModeChannels
	// Navigate to first actual channel (index 1, after All Activity).
	m.channelsView.selectedIdx = 1

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a cmd from Enter in ModeChannels")
	}
	msg := cmd()
	sel, ok := msg.(MsgChannelSelected)
	if !ok {
		t.Fatalf("expected MsgChannelSelected, got %T", msg)
	}
	if sel.ChannelID != "ch1" {
		t.Errorf("expected ChannelID=ch1, got %q", sel.ChannelID)
	}
}

// TestLayoutWidthSplit verifies that channels + divider + messages = total width.
func TestLayoutWidthSplit(t *testing.T) {
	m := initModel(t, NewModel())
	totalW := m.width
	chW := m.channelsView.width
	msgsW := m.messagesView.width
	if chW+msgsW+1 != totalW { // +1 for divider
		t.Errorf("width split: channels(%d) + divider(1) + messages(%d) != total(%d)", chW, msgsW, totalW)
	}
}

// TestChannelsViewOpenHighlight verifies that the open channel is shown in the rendered view.
func TestChannelsViewOpenHighlight(t *testing.T) {
	cv := NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
	})
	cv = cv.SetSize(22, 10)
	cv.selectedIdx = 1
	cv, _ = cv.OpenSelected()
	rendered := cv.View()
	if !strings.Contains(rendered, "general") {
		t.Error("rendered channels view should contain 'general'")
	}
}
