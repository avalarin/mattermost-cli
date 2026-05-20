package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
)

// --- SearchPopup unit tests ---

func TestSearchPopup_DefaultState(t *testing.T) {
	state := ChannelFilterState{SortOrder: ChannelSortLastMessage, UnreadOnly: true}
	p := NewSearchPopup(state, nil, nil)

	if p.Query() != "" {
		t.Errorf("expected empty query, got %q", p.Query())
	}
	if p.Filter().SortOrder != ChannelSortLastMessage {
		t.Error("filter SortOrder should match initial state")
	}
	if !p.Filter().UnreadOnly {
		t.Error("filter UnreadOnly should match initial state")
	}
	if p.Original().SortOrder != ChannelSortLastMessage {
		t.Error("original SortOrder should match initial state")
	}
}

func TestSearchPopupAlwaysShowsAllActivity(t *testing.T) {
	channels := []mattermost.Channel{
		{ID: "c1", Name: "general", DisplayName: "General", Type: "O"},
	}
	p := NewSearchPopup(ChannelFilterState{}, channels, nil)

	if len(p.results) < 1 {
		t.Fatal("expected at least one result (All Activity)")
	}
	if p.results[0].kind != searchResultAllActivity {
		t.Error("first result should always be All Activity")
	}
}

func TestSearchPopupShortQueryShowsLocal(t *testing.T) {
	channels := []mattermost.Channel{
		{ID: "c1", Name: "general", DisplayName: "General", Type: "O"},
		{ID: "c2", Name: "backend", DisplayName: "Backend", Type: "O"},
	}
	p := NewSearchPopup(ChannelFilterState{}, channels, nil)

	// One-char query — still local mode.
	p = p.TypeChar('g')
	if p.IsSearchMode() {
		t.Error("single char should not be search mode")
	}

	for _, item := range p.results {
		if item.kind == searchResultUser {
			t.Error("local mode should not contain user results")
		}
	}
}

func TestSearchPopup_TwoCharsIsSearchMode(t *testing.T) {
	p := NewSearchPopup(ChannelFilterState{}, nil, nil)
	p = p.TypeChar('g')
	p = p.TypeChar('e')
	if !p.IsSearchMode() {
		t.Error("two chars should activate search mode")
	}
}

func TestSearchPopup_BackspaceExitsSearchMode(t *testing.T) {
	p := NewSearchPopup(ChannelFilterState{}, nil, nil)
	p = p.TypeChar('g').TypeChar('e')
	if !p.IsSearchMode() {
		t.Fatal("expected search mode")
	}
	p = p.Backspace()
	if p.IsSearchMode() {
		t.Error("after backspace to 1 char, should not be search mode")
	}
}

func TestSearchPopup_SetSearchResults(t *testing.T) {
	p := NewSearchPopup(ChannelFilterState{}, nil, nil)
	p = p.TypeChar('g').TypeChar('e').SetSearching()

	channels := []mattermost.Channel{
		{ID: "c1", Name: "general", DisplayName: "General", Type: "O"},
	}
	users := []mattermost.User{
		{ID: "u1", Username: "george"},
	}
	p = p.SetSearchResults(channels, users)

	if p.searching {
		t.Error("searching should be false after SetSearchResults")
	}
	if len(p.results) != 3 { // All Activity + 1 channel + 1 user
		t.Errorf("expected 3 results, got %d", len(p.results))
	}
	if p.results[0].kind != searchResultAllActivity {
		t.Error("first result should be All Activity")
	}
	if p.results[1].kind != searchResultChannel {
		t.Error("second result should be a channel")
	}
	if p.results[2].kind != searchResultUser {
		t.Error("third result should be a user")
	}
	if !strings.HasPrefix(p.results[2].displayName, "@") {
		t.Errorf("user display name should start with @, got %q", p.results[2].displayName)
	}
}

func TestSearchPopup_Navigation(t *testing.T) {
	channels := []mattermost.Channel{
		{ID: "c1", DisplayName: "General", Type: "O"},
		{ID: "c2", DisplayName: "Backend", Type: "O"},
	}
	p := NewSearchPopup(ChannelFilterState{}, channels, nil)

	p = p.MoveDown()
	if p.cursor != 1 {
		t.Errorf("expected cursor=1, got %d", p.cursor)
	}

	// Clamp at bottom.
	p = p.MoveDown().MoveDown().MoveDown()
	if p.cursor >= len(p.results) {
		t.Error("cursor should not exceed results length")
	}

	// Clamp at top.
	for i := 0; i < 10; i++ {
		p = p.MoveUp()
	}
	if p.cursor != 0 {
		t.Errorf("cursor should clamp at 0, got %d", p.cursor)
	}
}

func TestSearchPopup_ToggleFocus(t *testing.T) {
	p := NewSearchPopup(ChannelFilterState{}, nil, nil)
	if p.focus != searchFocusResults {
		t.Error("initial focus should be results")
	}
	p = p.ToggleFocus()
	if p.focus != searchFocusFilter {
		t.Error("after Tab, focus should be filter")
	}
	p = p.ToggleFocus()
	if p.focus != searchFocusResults {
		t.Error("after second Tab, focus should be results again")
	}
}

func TestSearchPopup_ToggleFocusDisabledInSearchMode(t *testing.T) {
	p := NewSearchPopup(ChannelFilterState{}, nil, nil)
	p = p.TypeChar('g').TypeChar('e') // search mode
	p = p.ToggleFocus()
	if p.focus != searchFocusResults {
		t.Error("ToggleFocus should be no-op in search mode")
	}
}

func TestSearchPopup_ToggleFilter(t *testing.T) {
	p := NewSearchPopup(ChannelFilterState{SortOrder: ChannelSortAlphabetical}, nil, nil)
	p = p.ToggleFocus() // go to filter

	// Row 0 = Alphabetical (already set).
	p = p.ToggleFilter()
	if p.Filter().SortOrder != ChannelSortAlphabetical {
		t.Error("cursor=0 toggle should keep SortOrder=Alphabetical")
	}

	// Row 1 = Last message (move right within Sort row).
	p = p.MoveRight()
	p = p.ToggleFilter()
	if p.Filter().SortOrder != ChannelSortLastMessage {
		t.Error("cursor=1 toggle should set SortOrder=LastMessage")
	}

	// Row 2 = Unread toggle (move down to Filter row).
	p = p.MoveDown()
	p = p.ToggleFilter()
	if !p.Filter().UnreadOnly {
		t.Error("cursor=2 toggle should enable UnreadOnly")
	}
}

func TestSearchPopup_OriginalPreservedOnEsc(t *testing.T) {
	initial := ChannelFilterState{SortOrder: ChannelSortAlphabetical, UnreadOnly: false}
	p := NewSearchPopup(initial, nil, nil)
	p = p.ToggleFocus().MoveRight().ToggleFilter() // move to Last msg cursor, change sort to LastMessage

	if p.Filter().SortOrder != ChannelSortLastMessage {
		t.Error("pending SortOrder should be LastMessage")
	}
	if p.Original().SortOrder != ChannelSortAlphabetical {
		t.Error("original SortOrder should remain Alphabetical")
	}
}

func TestSearchPopup_SelectedItem(t *testing.T) {
	channels := []mattermost.Channel{
		{ID: "c1", DisplayName: "General", Type: "O"},
	}
	p := NewSearchPopup(ChannelFilterState{}, channels, nil)

	item, ok := p.SelectedItem()
	if !ok {
		t.Fatal("expected a selected item")
	}
	if item.kind != searchResultAllActivity {
		t.Error("default selected item should be All Activity")
	}
}

// --- Model integration tests ---

func TestCtrlKOpensSearchPopup(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})

	if m.searchPopup == nil {
		t.Error("expected searchPopup != nil after Ctrl+K")
	}
	if m.mode != ModeSearch {
		t.Errorf("expected ModeSearch, got %v", m.mode)
	}
}

func TestSearchPopupEscCloses(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})
	if m.searchPopup == nil {
		t.Fatal("searchPopup should be open after Ctrl+K")
	}

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEscape})

	if m.searchPopup != nil {
		t.Error("searchPopup should be nil after Esc")
	}
	if m.mode == ModeSearch {
		t.Error("mode should not be ModeSearch after Esc")
	}
}

func TestSearchPopupCtrlCCloses(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})
	if m.searchPopup == nil {
		t.Fatal("searchPopup should be open after Ctrl+K")
	}

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})

	if m.searchPopup != nil {
		t.Error("searchPopup should be nil after Ctrl+C in ModeSearch")
	}
}

func TestSearchPopupShortQueryInModel(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})

	if m.searchPopup == nil {
		t.Fatal("searchPopup should still be open")
	}
	if m.searchPopup.IsSearchMode() {
		t.Error("one character should not activate search mode")
	}
}

func TestSearchPopupAlwaysShowsAllActivityInModel(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})

	if m.searchPopup == nil {
		t.Fatal("expected searchPopup to be open")
	}
	if len(m.searchPopup.results) == 0 {
		t.Fatal("expected at least one result")
	}
	if m.searchPopup.results[0].kind != searchResultAllActivity {
		t.Error("first result should always be All Activity")
	}
}

func TestSearchPopupEnterOpensChannel(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)
	m.teamID = "team1"

	// Inject a channel into the channels view.
	channels := []mattermost.Channel{{ID: "c1", Name: "general", DisplayName: "General", Type: "O"}}
	m.channelsView = NewChannelsView(channels)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})
	if m.searchPopup == nil {
		t.Fatal("searchPopup should be open")
	}

	// Move down to the first channel (index 1, after All Activity).
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyDown})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustModel(t, updated)

	if m.searchPopup != nil {
		t.Error("searchPopup should be closed after Enter")
	}
	if cmd == nil {
		t.Fatal("expected a cmd")
	}
	msgs := runBatchCmd(cmd)
	foundSelected := false
	for _, msg := range msgs {
		if sel, ok := msg.(MsgChannelSelected); ok && sel.ChannelID == "c1" {
			foundSelected = true
		}
	}
	if !foundSelected {
		t.Errorf("expected MsgChannelSelected{ChannelID: c1}, got %v", msgs)
	}
}

func TestSearchPopupEnterOpensDM(t *testing.T) {
	dmChannelID := "dm-channel-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/users/me":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mattermost.User{ID: "self-user", Username: "me"})
		case "/api/v4/channels/direct":
			ch := mattermost.Channel{ID: dmChannelID, Type: "D"}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ch)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	m := NewModel()
	m = initModel(t, m)
	m.client = mattermost.NewClient(srv.URL, "test-token")
	m.teamID = "team1"

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})
	if m.searchPopup == nil {
		t.Fatal("searchPopup should be open")
	}

	// Inject a user result directly into the popup.
	user := mattermost.User{ID: "u1", Username: "alice"}
	p := *m.searchPopup
	p.results = append(p.results, searchResultItem{kind: searchResultUser, user: user, displayName: "@alice"})
	for p.cursor < len(p.results)-1 {
		p = p.MoveDown()
	}
	m.searchPopup = &p

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustModel(t, updated)

	if m.searchPopup != nil {
		t.Error("searchPopup should be closed after Enter on user")
	}
	if cmd == nil {
		t.Fatal("expected a cmd")
	}
	msgs := runBatchCmd(cmd)
	foundSelected := false
	for _, msg := range msgs {
		if sel, ok := msg.(MsgChannelSelected); ok && sel.ChannelID == dmChannelID {
			foundSelected = true
		}
	}
	if !foundSelected {
		t.Errorf("expected MsgChannelSelected{ChannelID: %s}, got %v", dmChannelID, msgs)
	}
}

// runBatchCmd executes a tea.Cmd and collects all messages it produces (handles tea.BatchMsg).
func runBatchCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			msgs = append(msgs, runBatchCmd(c)...)
		}
		return msgs
	}
	return []tea.Msg{msg}
}
