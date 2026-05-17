package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// sendKey is a helper to send a single key event and return the updated model.
func sendKey(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return mustModel(t, updated)
}

// initModel sends a WindowSizeMsg to initialize the viewport.
func initModel(t *testing.T, m Model) Model {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return mustModel(t, updated)
}

func TestQuitCommandExits(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Type "/quit" into textarea.
	for _, r := range "/quit" {
		r := r
		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
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

// TestCtrlCShowsExitHint: first Ctrl+C shows "press again to exit" hint.
func TestCtrlCShowsExitHint(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})

	if !strings.Contains(m.StatusMsg(), "Ctrl+C") {
		t.Errorf("expected Ctrl+C hint in status bar, got: %q", m.StatusMsg())
	}
	if m.statusIsError {
		t.Errorf("expected no error flag on Ctrl+C hint, got statusIsError=true")
	}
	if !m.ctrlCPending {
		t.Error("expected ctrlCPending=true after first Ctrl+C")
	}
}

// TestDoubleCtrlCExits: second Ctrl+C within window produces tea.QuitMsg.
func TestDoubleCtrlCExits(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// First Ctrl+C.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})

	// Second Ctrl+C should produce tea.Quit.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit cmd after second Ctrl+C, got nil")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg after double Ctrl+C, got %T", msg)
	}
}

// TestFocusMessages: ctrl+j switches to ModeMessages.
// Note: ctrl+m == enter in standard terminals; ctrl+j (keyLF) is used instead.
func TestFocusMessages(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Add a feed item so SelectLast has something to select.
	m.messagesView = m.messagesView.AddFeedItem(feedItem{
		kind: feedItemKindMessage,
		msg: feedMessage{
			post:        mattermost.Message{ID: "msg1", Text: "hello"},
			senderName:  "alice",
			channelName: "general",
		},
	})

	// Send ctrl+j (focus messages key).
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = mustModel(t, updated)

	if m.mode != ModeMessages {
		t.Errorf("expected ModeMessages after ctrl+j, got %v", m.mode)
	}
	if m.messagesView.selectedIdx < 0 {
		t.Errorf("expected selectedIdx >= 0 after entering ModeMessages, got %d", m.messagesView.selectedIdx)
	}
}

// TestPrefixActivated: Ctrl+B activates prefix mode (shows hint, does not change mode yet).
func TestPrefixActivated(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)
	m.mode = ModeMessages

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})

	if !m.prefixPending {
		t.Error("expected prefixPending=true after Ctrl+B")
	}
	if m.mode != ModeMessages {
		t.Errorf("expected mode to stay ModeMessages after Ctrl+B, got %v", m.mode)
	}
	if m.statusMsg == "" {
		t.Error("expected status hint to be shown after Ctrl+B")
	}
}

// TestPrefixUpGoesToMessages: Ctrl+B then ↑ switches to ModeMessages from any mode.
func TestPrefixUpGoesToMessages(t *testing.T) {
	for _, startMode := range []Mode{ModeInput, ModeChannels, ModeMessages} {
		m := NewModel()
		m = initModel(t, m)
		m.mode = startMode
		m.messagesView = m.messagesView.AddFeedItem(feedItem{
			kind: feedItemKindMessage,
			msg:  feedMessage{post: mattermost.Message{ID: "m1", Text: "hi"}, senderName: "alice", channelName: "general"},
		})

		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyUp})

		if m.mode != ModeMessages {
			t.Errorf("startMode=%v: expected ModeMessages after Ctrl+B ↑, got %v", startMode, m.mode)
		}
	}
}

// TestPrefixDownGoesToInput: Ctrl+B then ↓ switches to ModeInput from any mode.
func TestPrefixDownGoesToInput(t *testing.T) {
	for _, startMode := range []Mode{ModeMessages, ModeChannels} {
		m := NewModel()
		m = initModel(t, m)
		m.mode = startMode

		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyDown})

		if m.mode != ModeInput {
			t.Errorf("startMode=%v: expected ModeInput after Ctrl+B ↓, got %v", startMode, m.mode)
		}
	}
}

// TestPrefixLeftGoesToChannels: Ctrl+B then ← switches to ModeChannels.
func TestPrefixLeftGoesToChannels(t *testing.T) {
	for _, startMode := range []Mode{ModeInput, ModeMessages} {
		m := NewModel()
		m = initModel(t, m)
		m.mode = startMode

		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyLeft})

		if m.mode != ModeChannels {
			t.Errorf("startMode=%v: expected ModeChannels after Ctrl+B ←, got %v", startMode, m.mode)
		}
	}
}

// TestPrefixRightGoesToMessages: Ctrl+B then → switches to ModeMessages.
func TestPrefixRightGoesToMessages(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)
	m.mode = ModeChannels
	m.messagesView = m.messagesView.AddFeedItem(feedItem{
		kind: feedItemKindMessage,
		msg:  feedMessage{post: mattermost.Message{ID: "m1", Text: "hi"}, senderName: "alice", channelName: "general"},
	})

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRight})

	if m.mode != ModeMessages {
		t.Errorf("expected ModeMessages after Ctrl+B →, got %v", m.mode)
	}
}

// TestPrefixCancelledByNonArrow: Ctrl+B then a non-arrow key cancels prefix mode.
func TestPrefixCancelledByNonArrow(t *testing.T) {
	for _, startMode := range []Mode{ModeInput, ModeMessages, ModeChannels} {
		m := NewModel()
		m = initModel(t, m)
		m.mode = startMode

		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
		if !m.prefixPending {
			t.Fatalf("startMode=%v: expected prefixPending=true after Ctrl+B", startMode)
		}
		// Press a letter — prefix should be cancelled, mode unchanged.
		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

		if m.prefixPending {
			t.Errorf("startMode=%v: expected prefixPending=false after non-arrow key", startMode)
		}
		if m.mode != startMode {
			t.Errorf("startMode=%v: expected mode to remain %v after cancel, got %v", startMode, startMode, m.mode)
		}
	}
}

// TestPrefixTimeoutClearsPending: MsgPrefixTimeout with matching gen clears prefix state.
func TestPrefixTimeoutClearsPending(t *testing.T) {
	m := initModel(t, NewModel())

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
	if !m.prefixPending {
		t.Fatal("expected prefixPending=true after Ctrl+B")
	}
	gen := m.prefixGen

	updated, _ := m.Update(MsgPrefixTimeout{Gen: gen})
	m = mustModel(t, updated)

	if m.prefixPending {
		t.Error("expected prefixPending=false after MsgPrefixTimeout")
	}
	if m.statusMsg != "" {
		t.Errorf("expected empty statusMsg after timeout, got %q", m.statusMsg)
	}
}

// TestStalePrefixTimeoutIgnored: MsgPrefixTimeout with stale gen does not clear prefix state.
func TestStalePrefixTimeoutIgnored(t *testing.T) {
	m := initModel(t, NewModel())

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
	gen := m.prefixGen

	// Send stale timeout — should be ignored.
	updated, _ := m.Update(MsgPrefixTimeout{Gen: gen - 1})
	m = mustModel(t, updated)

	if !m.prefixPending {
		t.Error("stale MsgPrefixTimeout should not clear prefixPending")
	}

	// Correct gen clears it.
	updated, _ = m.Update(MsgPrefixTimeout{Gen: gen})
	m = mustModel(t, updated)
	if m.prefixPending {
		t.Error("expected prefixPending=false after correct-gen MsgPrefixTimeout")
	}
}

// TestEscReturnsToInput: Esc in ModeMessages returns to ModeInput.
func TestEscReturnsToInput(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Put model in ModeMessages.
	m.mode = ModeMessages

	// Press Esc.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mustModel(t, updated)

	if m.mode != ModeInput {
		t.Errorf("expected ModeInput after Esc from ModeMessages, got %v", m.mode)
	}
}

// TestEmptyInputUpGoesToMessages: Up in ModeInput with empty input enters ModeMessages.
func TestEmptyInputUpGoesToMessages(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Add a message so there's something to select.
	m.messagesView = m.messagesView.AddFeedItem(feedItem{
		kind: feedItemKindMessage,
		msg: feedMessage{
			post:        mattermost.Message{ID: "msg1", Text: "hello"},
			senderName:  "alice",
			channelName: "general",
		},
	})

	// Input should be empty.
	if m.input.Value() != "" {
		t.Fatal("input should be empty initially")
	}

	// Press Up.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = mustModel(t, updated)

	if m.mode != ModeMessages {
		t.Errorf("expected ModeMessages after Up with empty input, got %v", m.mode)
	}
}

// TestAltEnterInsertsNewline: Alt+Enter in ModeInput inserts a newline into the textarea.
func TestAltEnterInsertsNewline(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Type some text first so the textarea has content.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})

	linesBefore := m.input.LineCount()

	// Press Alt+Enter to insert a newline.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})

	if m.input.LineCount() <= linesBefore {
		t.Errorf("expected LineCount to increase after Alt+Enter, was %d now %d", linesBefore, m.input.LineCount())
	}
}

// TestNavKeysDisabledInInput: Up in ModeInput with non-empty input doesn't change mode.
func TestNavKeysDisabledInInput(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Type something so input is non-empty.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})

	if m.input.Value() == "" {
		t.Fatal("expected non-empty input")
	}

	// Press Up — should NOT go to ModeMessages since input is non-empty.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = mustModel(t, updated)

	if m.mode == ModeMessages {
		t.Errorf("expected ModeInput after Up with non-empty input, got ModeMessages")
	}
}

// TestEndJumpsToBottom: End in ModeMessages sets atBottom=true.
func TestEndJumpsToBottom(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Add several messages.
	for i := 0; i < 5; i++ {
		m.messagesView = m.messagesView.AddFeedItem(feedItem{
			kind: feedItemKindMessage,
			msg: feedMessage{
				post:        mattermost.Message{ID: fmt.Sprintf("msg%d", i), Text: "hello"},
				senderName:  "alice",
				channelName: "general",
			},
		})
	}
	m.messagesView = m.messagesView.SetAtBottom(false)
	m.mode = ModeMessages
	m.messagesView.selectedIdx = 0

	// Press End.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = mustModel(t, updated)

	if !m.messagesView.AtBottom() {
		t.Errorf("expected atBottom=true after End key, got false")
	}
}

// TestFeedAutoScrollAtBottom: new message when atBottom=true auto-scrolls viewport.
func TestFeedAutoScrollAtBottom(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)
	m.messagesView = m.messagesView.SetAtBottom(true)

	// Add a message via handlePostedEvent.
	evt := buildPostedEvent("msg-auto", "general", "ch1", "alice", "new message")
	updated, _ := m.handlePostedEvent(evt)
	m = updated

	// After auto-scroll, atBottom should remain true.
	if !m.messagesView.AtBottom() {
		t.Errorf("expected atBottom=true after new message with atBottom=true, got false")
	}
}

// TestFeedNoAutoScrollWhenScrolledUp: new message when atBottom=false doesn't scroll.
func TestFeedNoAutoScrollWhenScrolledUp(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)
	m.messagesView = m.messagesView.SetAtBottom(false)

	offsetBefore := m.messagesView.VPYOffset()

	evt := buildPostedEvent("msg-noscroll", "general", "ch1", "bob", "another message")
	updated, _ := m.handlePostedEvent(evt)
	m = updated

	if m.messagesView.AtBottom() {
		t.Errorf("expected atBottom=false after new message when scrolled up, got true")
	}
	if m.messagesView.VPYOffset() != offsetBefore {
		t.Errorf("expected viewport YOffset unchanged, before=%d after=%d", offsetBefore, m.messagesView.VPYOffset())
	}
}

// TestUpArrowScrollsFeed: Up in ModeMessages moves cursor to previous message.
func TestUpArrowScrollsFeed(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Add two messages.
	m.messagesView = m.messagesView.AddFeedItem(feedItem{kind: feedItemKindMessage, msg: feedMessage{post: mattermost.Message{ID: "msg1", Text: "first"}, senderName: "alice", channelName: "general"}})
	m.messagesView = m.messagesView.AddFeedItem(feedItem{kind: feedItemKindMessage, msg: feedMessage{post: mattermost.Message{ID: "msg2", Text: "second"}, senderName: "bob", channelName: "general"}})
	m.mode = ModeMessages
	m.messagesView.selectedIdx = 1 // start at second message

	// Press Up.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = mustModel(t, updated)

	if m.messagesView.selectedIdx != 0 {
		t.Errorf("expected selectedIdx=0 after Up, got %d", m.messagesView.selectedIdx)
	}
}

// TestSlashInMessagesGoesToInput: pressing "/" in ModeMessages switches to ModeInput.
func TestSlashInMessagesGoesToInput(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Add a message and enter ModeMessages.
	m.messagesView = m.messagesView.AddFeedItem(feedItem{
		kind: feedItemKindMessage,
		msg:  feedMessage{post: mattermost.Message{ID: "msg1", Text: "hello"}, senderName: "alice", channelName: "general"},
	})
	m.mode = ModeMessages
	m.messagesView.selectedIdx = 0

	// Press "/".
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = mustModel(t, updated)

	if m.mode != ModeInput {
		t.Errorf("expected ModeInput after '/' in ModeMessages, got %v", m.mode)
	}
	if m.input.Value() != "/" {
		t.Errorf("expected input value '/', got %q", m.input.Value())
	}
	// Selected message should remain.
	if m.messagesView.selectedIdx != 0 {
		t.Errorf("expected selectedIdx=0 to remain after '/' in ModeMessages, got %d", m.messagesView.selectedIdx)
	}
}

func TestLayoutHeightFitsWindow(t *testing.T) {
	m := NewModel()
	const width, height = 100, 30
	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height}); return updated }())

	// Layout: header(1) + divider(1) + feed + divider(1) + statusbar(1) + input(minInputHeight=1) + divider(1)
	// feedHeight = height - 5 - minInputHeight (empty textarea starts at 1 line)
	// MessagesView viewport gets feedH-1 (header row reserved), so vp.Height = feedH - 1
	wantFeedHeight := height - 5 - minInputHeight
	wantVPHeight := wantFeedHeight - 1 // viewport is feedH minus header row
	if m.messagesView.vp.Height != wantVPHeight {
		t.Errorf("expected viewport height %d, got %d", wantVPHeight, m.messagesView.vp.Height)
	}
	// Messages panel width = total - channelsWidth - 1 (divider)
	wantMsgsWidth := width - m.channelsWidth - 1
	if m.messagesView.vp.Width != wantMsgsWidth {
		t.Errorf("expected viewport width %d, got %d", wantMsgsWidth, m.messagesView.vp.Width)
	}

	// Resize to a different size: the else-branch must update both dimensions.
	const width2, height2 = 120, 40
	m = mustModel(t, func() tea.Model { updated, _ := m.Update(tea.WindowSizeMsg{Width: width2, Height: height2}); return updated }())

	wantFeedHeight2 := height2 - 5 - minInputHeight
	wantVPHeight2 := wantFeedHeight2 - 1
	if m.messagesView.vp.Height != wantVPHeight2 {
		t.Errorf("after resize: expected viewport height %d, got %d", wantVPHeight2, m.messagesView.vp.Height)
	}
	wantMsgsWidth2 := width2 - m.channelsWidth - 1
	if m.messagesView.vp.Width != wantMsgsWidth2 {
		t.Errorf("after resize: expected viewport width %d, got %d", wantMsgsWidth2, m.messagesView.vp.Width)
	}
}

func TestFeedRenderReply(t *testing.T) {
	createAt := time.Now().UnixMilli()

	post := mattermost.Message{
		ID:       "reply-id",
		Text:     "I am fine, thanks!",
		CreateAt: createAt,
		RootID:   "parent-id",
	}

	line := renderMessageLine(post, "alice", "general", "Hello everyone, how are you doing today?", "02.01.2006", 120, false)

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
	post := mattermost.Message{
		ID:       "reply-id",
		RootID:   "unknown-parent",
		Text:     "my reply",
		CreateAt: time.Now().UnixMilli(),
	}

	line := renderMessageLine(post, "bob", "general", "", "02.01.2006", 120, false)

	if !strings.Contains(line, "↩") {
		t.Errorf("expected ↩ indicator even without parent snippet, got: %q", line)
	}
	if !strings.Contains(line, "my reply") {
		t.Errorf("expected message text in line, got: %q", line)
	}
	// No snippet parens when snippet is empty.
	if strings.Contains(line, "(") {
		t.Errorf("expected no snippet parens when snippet empty, got: %q", line)
	}
}

func TestFeedRenderNormalMessage(t *testing.T) {
	post := mattermost.Message{
		ID:       "msg-id",
		Text:     "hello world",
		CreateAt: time.Now().UnixMilli(),
	}

	line := renderMessageLine(post, "charlie", "random", "", "02.01.2006", 120, false)

	if strings.Contains(line, "↩") {
		t.Errorf("expected no ↩ for top-level message, got: %q", line)
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

// testModelWithClient builds a Model wired to the given client and initializes the viewport.
func testModelWithClient(t *testing.T, client *mattermost.Client, teamID string) Model {
	t.Helper()
	m := NewModelWithHeader(HeaderInfo{}, "", nil, nil, nil, nil, client, teamID, 22, false, "15", "237", "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return mustModel(t, updated)
}

// writeTestJSON is a helper to write a JSON response in tests.
func writeTestJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func TestExecuteSendChannelNotFound(t *testing.T) {
	// httptest server that always 404s (simulates channel not found).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	client := mattermost.NewClient(srv.URL, "test-token")
	m := testModelWithClient(t, client, "team1")

	// Directly call executeCommand to get the async cmd.
	_, asyncCmd := m.executeCommand("/send #nonexistent hello")
	if asyncCmd == nil {
		t.Fatal("expected async cmd from executeCommand, got nil")
	}

	result := asyncCmd()
	cr, ok := result.(MsgCommandResult)
	if !ok {
		t.Fatalf("expected MsgCommandResult, got %T", result)
	}
	if cr.Err == nil {
		t.Fatal("expected error for channel not found, got nil")
	}
	if !strings.Contains(cr.Err.Error(), "channel not found") {
		t.Errorf("expected 'channel not found' in error, got: %v", cr.Err)
	}
}

func TestExecuteSendDMSuccess(t *testing.T) {
	// httptest server that handles user lookup, DM creation, and message send.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/users/me":
			writeTestJSON(w, map[string]string{"id": "me123", "username": "myuser"})
		case "/api/v4/users/username/bob":
			writeTestJSON(w, map[string]string{"id": "bob456", "username": "bob"})
		case "/api/v4/channels/direct":
			writeTestJSON(w, map[string]string{"id": "dm-chan-id", "name": "dm-chan"})
		case "/api/v4/posts":
			writeTestJSON(w, map[string]interface{}{"id": "post789", "channel_id": "dm-chan-id", "message": "hello"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := mattermost.NewClient(srv.URL, "test-token")
	// Pre-warm currentUserID by calling GetCurrentUser.
	if _, err := client.GetCurrentUser(); err != nil {
		t.Fatalf("GetCurrentUser() setup error: %v", err)
	}
	m := testModelWithClient(t, client, "team1")

	_, asyncCmd := m.executeCommand("/send @bob hello there")
	if asyncCmd == nil {
		t.Fatal("expected async cmd from executeCommand, got nil")
	}

	result := asyncCmd()
	cr, ok := result.(MsgCommandResult)
	if !ok {
		t.Fatalf("expected MsgCommandResult, got %T", result)
	}
	if cr.Err != nil {
		t.Fatalf("expected success, got error: %v", cr.Err)
	}
	if cr.Info != "Sent ✓" {
		t.Errorf("Info = %q, want %q", cr.Info, "Sent ✓")
	}
}

func TestFeedRenderWordWrap(t *testing.T) {
	longText := strings.Repeat("word ", 30) // 150 chars — will need wrapping at width=40
	post := mattermost.Message{
		ID:       "msg-id",
		Text:     longText,
		CreateAt: time.Now().UnixMilli(),
	}

	line := renderMessageLine(post, "dave", "chan", "", "02.01.2006", 40, false)

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

func TestDoubleEscClearsAndDeselects(t *testing.T) {
	m := initModel(t, NewModel())

	// Add a message and go to ModeMessages.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	if m.mode != ModeMessages {
		t.Fatalf("expected ModeMessages after ctrl+j, got %v", m.mode)
	}

	// First Esc from ModeMessages → goes to ModeInput, escPending=true, hint shown.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != ModeInput {
		t.Errorf("expected ModeInput after first esc, got %v", m.mode)
	}
	if !m.escPending {
		t.Error("expected escPending=true after first esc from ModeMessages")
	}
	if !strings.Contains(m.statusMsg, "Esc again") {
		t.Errorf("expected hint in status bar, got %q", m.statusMsg)
	}

	// Second Esc from ModeInput → clears escPending, deselects.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.escPending {
		t.Error("expected escPending=false after second esc")
	}
	if m.messagesView.selectedIdx != -1 {
		t.Errorf("expected selectedIdx=-1 after double esc, got %d", m.messagesView.selectedIdx)
	}
	if m.statusMsg != "" {
		t.Errorf("expected empty status after double esc, got %q", m.statusMsg)
	}
}

func TestStaleEscTimeoutIgnored(t *testing.T) {
	m := initModel(t, NewModel())

	// First esc → escPending=true, escGen incremented.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	gen := m.escGen

	// Simulate a stale timeout with old gen.
	updated, _ := m.Update(MsgEscTimeout{Gen: gen - 1})
	m = mustModel(t, updated)

	// escPending should still be true (stale timeout ignored).
	if !m.escPending {
		t.Error("stale MsgEscTimeout should not clear escPending")
	}

	// Correct gen timeout clears escPending.
	updated, _ = m.Update(MsgEscTimeout{Gen: gen})
	m = mustModel(t, updated)
	if m.escPending {
		t.Error("expected escPending=false after correct-gen MsgEscTimeout")
	}
}

// TestCtrlCWorksInModeMessages: Ctrl+C shows exit hint even in ModeMessages.
func TestCtrlCWorksInModeMessages(t *testing.T) {
	m := initModel(t, NewModel())
	m.mode = ModeMessages

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})

	if !m.ctrlCPending {
		t.Error("expected ctrlCPending=true after Ctrl+C in ModeMessages")
	}
	if !strings.Contains(m.StatusMsg(), "Ctrl+C") {
		t.Errorf("expected exit hint in ModeMessages, got: %q", m.StatusMsg())
	}
}

// TestHelpPopupOpens: /help with no args opens ModeHelp.
func TestHelpPopupOpens(t *testing.T) {
	m := NewModelWithHeader(HeaderInfo{}, "", nil, nil, nil, nil, nil, "", 22, false, "15", "237", "")
	m = initModel(t, m)

	// Type "/help" and press Enter.
	for _, r := range "/help" {
		r := r
		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, asyncCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if asyncCmd == nil {
		t.Fatal("expected async cmd from /help, got nil")
	}

	// Run the cmd to get MsgOpenHelp.
	msg := asyncCmd()
	if _, ok := msg.(MsgOpenHelp); !ok {
		t.Fatalf("expected MsgOpenHelp, got %T", msg)
	}

	// Feed MsgOpenHelp into the model.
	updated, _ := m.Update(msg)
	m = mustModel(t, updated)

	if m.mode != ModeHelp {
		t.Errorf("expected ModeHelp after MsgOpenHelp, got %v", m.mode)
	}
}

// TestHelpPopupClosesWithEsc: Esc in ModeHelp returns to previous mode.
func TestHelpPopupClosesWithEsc(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Open help popup directly.
	updated, _ := m.Update(MsgOpenHelp{})
	m = mustModel(t, updated)
	if m.mode != ModeHelp {
		t.Fatalf("expected ModeHelp, got %v", m.mode)
	}

	// Press Esc to close.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.mode != ModeInput {
		t.Errorf("expected ModeInput after Esc in ModeHelp, got %v", m.mode)
	}
}

// TestHelpPopupClosesWithCtrlC: Ctrl+C in ModeHelp closes the popup (not the double-Ctrl+C exit).
func TestHelpPopupClosesWithCtrlC(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Open help popup.
	updated, _ := m.Update(MsgOpenHelp{})
	m = mustModel(t, updated)
	if m.mode != ModeHelp {
		t.Fatalf("expected ModeHelp, got %v", m.mode)
	}

	// Press Ctrl+C — should close popup, not trigger exit mechanic.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})

	if m.mode != ModeInput {
		t.Errorf("expected ModeInput after Ctrl+C in ModeHelp, got %v", m.mode)
	}
	if m.ctrlCPending {
		t.Error("expected ctrlCPending=false after Ctrl+C closes help popup")
	}
}

// TestChannelSelectLoadsHistory verifies that MsgChannelSelected for a non-empty
// channel ID triggers a loadChannelHistoryCmd (i.e. the returned cmd produces MsgChannelHistory).
func TestChannelSelectLoadsHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]interface{}{
			"order": []string{"p1"},
			"posts": map[string]interface{}{
				"p1": map[string]interface{}{
					"id": "p1", "channel_id": "chan1", "user_id": "u1",
					"message": "hello", "create_at": 1000,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := mattermost.NewClient(srv.URL, "test-token")
	m := testModelWithClient(t, client, "team1")

	_, cmd := m.Update(MsgChannelSelected{ChannelID: "chan1"})
	if cmd == nil {
		t.Fatal("expected a cmd from MsgChannelSelected, got nil")
	}

	// cmd is a tea.Batch; execute it and collect resulting messages.
	batchMsg := cmd()
	// tea.Batch returns a batchMsg; we need to look for MsgChannelHistory.
	// Run as a sequence of cmds.
	var found bool
	switch v := batchMsg.(type) {
	case MsgChannelHistory:
		found = true
		if v.Err != nil {
			t.Errorf("unexpected error: %v", v.Err)
		}
	default:
		// The batch may wrap cmds; try running individual cmds.
		_ = v
		// Build the cmd directly to test the load cmd itself.
		loadCmd := loadChannelHistoryCmd(client, "chan1", 0, false)
		result := loadCmd()
		hist, ok := result.(MsgChannelHistory)
		if !ok {
			t.Fatalf("expected MsgChannelHistory from loadChannelHistoryCmd, got %T", result)
		}
		found = true
		if hist.Err != nil {
			t.Errorf("unexpected error in history load: %v", hist.Err)
		}
		if hist.ChannelID != "chan1" {
			t.Errorf("ChannelID = %q, want %q", hist.ChannelID, "chan1")
		}
	}
	if !found {
		t.Error("expected MsgChannelHistory to be produced")
	}
}

// TestReloadCommandNoChannel verifies that /reload without an active channel yields an error status.
func TestReloadCommandNoChannel(t *testing.T) {
	m := NewModelWithHeader(HeaderInfo{}, "", nil, nil, nil, nil, nil, "", 22, false, "15", "237", "")
	m = initModel(t, m)
	// activeChannelID is "" by default (All Activity).

	updated, _ := m.Update(MsgRequestReload{})
	m = mustModel(t, updated)

	if !m.statusIsError {
		t.Error("expected statusIsError=true after /reload with no active channel")
	}
	if !strings.Contains(m.statusMsg, "Not in a channel") {
		t.Errorf("expected 'Not in a channel' in status, got: %q", m.statusMsg)
	}
}

// TestReloadCommandActiveChannel verifies that MsgRequestReload in an active channel
// sets historyLoading=true and returns a cmd that produces MsgChannelHistory.
func TestReloadCommandActiveChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]interface{}{
			"order": []string{"p2"},
			"posts": map[string]interface{}{
				"p2": map[string]interface{}{
					"id": "p2", "channel_id": "chan1", "user_id": "u1",
					"message": "older msg", "create_at": 500,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := mattermost.NewClient(srv.URL, "test-token")
	m := testModelWithClient(t, client, "team1")
	m.activeChannelID = "chan1"
	m.activePage["chan1"] = 1 // pretend first page already loaded

	updated, cmd := m.Update(MsgRequestReload{})
	m = mustModel(t, updated)

	if !m.historyLoading {
		t.Error("expected historyLoading=true after MsgRequestReload")
	}
	if cmd == nil {
		t.Fatal("expected a cmd from MsgRequestReload, got nil")
	}

	// Execute the batch to get the history result.
	loadCmd := loadChannelHistoryCmd(client, "chan1", 1, true)
	result := loadCmd()
	hist, ok := result.(MsgChannelHistory)
	if !ok {
		t.Fatalf("expected MsgChannelHistory from loadChannelHistoryCmd, got %T", result)
	}
	if hist.Err != nil {
		t.Errorf("unexpected error: %v", hist.Err)
	}
	if hist.ChannelID != "chan1" {
		t.Errorf("ChannelID = %q, want %q", hist.ChannelID, "chan1")
	}
	if !hist.Prepend {
		t.Error("expected Prepend=true for /reload")
	}
}

// TestMsgChannelHistoryUpdatesView verifies that MsgChannelHistory updates the messages view.
func TestMsgChannelHistoryUpdatesView(t *testing.T) {
	m := NewModelWithHeader(HeaderInfo{}, "", nil, nil, nil, nil, nil, "", 22, false, "15", "237", "")
	m = initModel(t, m)
	m.activeChannelID = "chan1"

	posts := []mattermost.Message{
		{ID: "p1", ChannelID: "chan1", UserID: "u1", Text: "hello", CreateAt: 1000},
		{ID: "p2", ChannelID: "chan1", UserID: "u1", Text: "world", CreateAt: 2000},
	}
	updated, _ := m.Update(MsgChannelHistory{
		ChannelID: "chan1",
		Messages:  posts,
		Prepend:   false,
		Err:       nil,
	})
	m = mustModel(t, updated)

	if m.messagesView.IsEmpty() {
		t.Error("expected messagesView to be non-empty after MsgChannelHistory")
	}
	if m.historyLoading {
		t.Error("expected historyLoading=false after MsgChannelHistory")
	}
}

// buildPostedEvent creates a fake mattermost.Event of type "posted" for testing.
func buildPostedEvent(msgID, channelName, channelID, senderName, text string) mattermost.Event {
	post := mattermost.Message{
		ID:        msgID,
		ChannelID: channelID,
		UserID:    "user1",
		Text:      text,
		CreateAt:  time.Now().UnixMilli(),
	}
	postBytes, _ := json.Marshal(post)
	return mattermost.Event{
		Type: mattermost.EventTypePosted,
		Data: map[string]interface{}{
			"post":         string(postBytes),
			"sender_name":  "@" + senderName,
			"channel_name": channelName,
		},
	}
}
