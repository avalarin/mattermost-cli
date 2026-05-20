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

// TestChannelsViewRendersUnreadBadge verifies that a channel with unread count > 0
// shows the count badge in the rendered view.
func TestChannelsViewRendersUnreadBadge(t *testing.T) {
	cv := NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
	})
	cv = cv.SetSize(30, 10)
	cv = cv.SetUnreadCounts(map[string]int{"ch1": 3})

	rendered := cv.View()
	if !strings.Contains(rendered, "(3)") {
		t.Errorf("expected unread badge '(3)' in rendered view, got:\n%s", rendered)
	}
}

// TestChannelsViewNoBadgeWhenZero verifies that a channel with unread count == 0
// does not show a badge.
func TestChannelsViewNoBadgeWhenZero(t *testing.T) {
	cv := NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
	})
	cv = cv.SetSize(22, 10)
	cv = cv.SetUnreadCounts(map[string]int{"ch1": 0})

	rendered := cv.View()
	if strings.Contains(rendered, "(0)") {
		t.Errorf("should not show badge when count == 0, got:\n%s", rendered)
	}
}

// TestAllActivityNoBadge verifies that All Activity never shows an unread badge.
func TestAllActivityNoBadge(t *testing.T) {
	cv := NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
	})
	cv = cv.SetSize(22, 10)
	// No counts for All Activity sentinel ID "".
	cv = cv.SetUnreadCounts(map[string]int{"": 99})

	rendered := cv.View()
	// The first line (header) + second line (All Activity) should not show a number.
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		if strings.Contains(line, "All Activity") && strings.Contains(line, "(99)") {
			t.Errorf("line %d: All Activity should not show badge, got: %q", i, line)
		}
	}
}

func TestWithSortFilter_AllActivityAlwaysFirst(t *testing.T) {
	cv := NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general", LastPostAt: 100},
		{ID: "ch2", Name: "backend", LastPostAt: 200},
	})
	state := ChannelFilterState{SortOrder: ChannelSortLastMessage}
	cv = cv.WithSortAndFilter(state, nil)
	if !cv.items[0].isAll {
		t.Error("first item should be All Activity regardless of sort")
	}
}

func TestWithSortFilter_Alphabetical(t *testing.T) {
	cv := NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "zzz", DisplayName: "Zzz"},
		{ID: "ch2", Name: "aaa", DisplayName: "Aaa"},
	})
	state := ChannelFilterState{SortOrder: ChannelSortAlphabetical}
	cv = cv.WithSortAndFilter(state, nil)
	if cv.items[1].channel.ID != "ch2" {
		t.Errorf("expected ch2 (Aaa) at index 1, got %q", cv.items[1].channel.ID)
	}
	if cv.items[2].channel.ID != "ch1" {
		t.Errorf("expected ch1 (Zzz) at index 2, got %q", cv.items[2].channel.ID)
	}
}

func TestWithSortFilter_LastMessage(t *testing.T) {
	cv := NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "old", LastPostAt: 100},
		{ID: "ch2", Name: "newer", LastPostAt: 500},
		{ID: "ch3", Name: "newest", LastPostAt: 999},
	})
	state := ChannelFilterState{SortOrder: ChannelSortLastMessage}
	cv = cv.WithSortAndFilter(state, nil)
	if cv.items[1].channel.ID != "ch3" {
		t.Errorf("expected ch3 (newest) at index 1, got %q", cv.items[1].channel.ID)
	}
	if cv.items[3].channel.ID != "ch1" {
		t.Errorf("expected ch1 (old) at index 3, got %q", cv.items[3].channel.ID)
	}
}

func TestWithSortFilter_UnreadOnly(t *testing.T) {
	cv := NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
		{ID: "ch2", Name: "backend"},
		{ID: "ch3", Name: "ops"},
	})
	unreadCounts := map[string]int{"ch1": 3, "ch2": 0, "ch3": 1}
	state := ChannelFilterState{SortOrder: ChannelSortAlphabetical, UnreadOnly: true}
	cv = cv.WithSortAndFilter(state, unreadCounts)
	if len(cv.items) != 3 {
		t.Fatalf("expected 3 items (All Activity + ch1 + ch3), got %d", len(cv.items))
	}
	for _, item := range cv.items[1:] {
		if item.channel.ID == "ch2" {
			t.Error("ch2 with 0 unreads should be hidden")
		}
	}
}

func TestWithSortFilter_UnreadOnlyAllRead(t *testing.T) {
	cv := NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
		{ID: "ch2", Name: "backend"},
	})
	state := ChannelFilterState{SortOrder: ChannelSortAlphabetical, UnreadOnly: true}
	cv = cv.WithSortAndFilter(state, map[string]int{})
	if len(cv.items) != 1 {
		t.Fatalf("expected only All Activity (1 item), got %d", len(cv.items))
	}
	if !cv.items[0].isAll {
		t.Error("the single remaining item should be All Activity")
	}
}

func TestArchivedChannelRendersWithMarker(t *testing.T) {
	cv := NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "archived-channel", DeleteAt: 1234567890},
	})
	cv = cv.SetSize(22, 10)
	cv = cv.WithSortAndFilter(ChannelFilterState{}, nil)
	rendered := cv.View()
	if !strings.Contains(rendered, "[x]") {
		t.Errorf("archived channel should render with [x] marker, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "archived-channel") {
		t.Errorf("archived channel name should appear in rendered view, got:\n%s", rendered)
	}
}

func TestInfoKeyOpensPopup(t *testing.T) {
	m := initModel(t, NewModel())
	m.channelsView = NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
	})
	m.channelsView = m.channelsView.SetSize(22, 20)
	m.channelsView = m.channelsView.WithSortAndFilter(ChannelFilterState{}, nil)
	m.channelsView = m.channelsView.MoveDown() // select ch1 (index 1)
	m.mode = ModeChannels

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if m.infoPopup == nil {
		t.Fatal("expected infoPopup to be non-nil after pressing i in ModeChannels")
	}
	if m.mode != ModeInfo {
		t.Errorf("expected ModeInfo after pressing i, got %v", m.mode)
	}
}

func TestInfoPopupEscCloses(t *testing.T) {
	m := initModel(t, NewModel())
	m.channelsView = NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
	})
	m.channelsView = m.channelsView.SetSize(22, 20)
	m.channelsView = m.channelsView.MoveDown()
	m.mode = ModeChannels

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if m.infoPopup == nil {
		t.Fatal("expected infoPopup open before Esc")
	}

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.infoPopup != nil {
		t.Error("expected infoPopup to be nil after Esc")
	}
	if m.mode != ModeChannels {
		t.Errorf("expected ModeChannels after Esc from info popup, got %v", m.mode)
	}
}

func TestInfoPopupEnterCloses(t *testing.T) {
	m := initModel(t, NewModel())
	m.channelsView = NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "general"},
	})
	m.channelsView = m.channelsView.SetSize(22, 20)
	m.channelsView = m.channelsView.MoveDown()
	m.mode = ModeChannels

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if m.infoPopup == nil {
		t.Fatal("expected infoPopup open before Enter")
	}

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.infoPopup != nil {
		t.Error("expected infoPopup to be nil after Enter")
	}
	if m.mode != ModeChannels {
		t.Errorf("expected ModeChannels after Enter from info popup, got %v", m.mode)
	}
}

func TestArchivedChannelEnterOpens(t *testing.T) {
	m := initModel(t, NewModel())
	m.channelsView = NewChannelsView([]mattermost.Channel{
		{ID: "ch1", Name: "archived-channel", DeleteAt: 1234567890},
	})
	m.channelsView = m.channelsView.SetSize(22, 20)
	m.channelsView = m.channelsView.WithSortAndFilter(ChannelFilterState{}, nil)
	m.channelsView = m.channelsView.MoveDown() // move cursor from All Activity to the archived channel
	m.mode = ModeChannels

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a cmd from Enter on archived channel")
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
