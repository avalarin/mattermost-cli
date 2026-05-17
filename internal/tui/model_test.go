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

// TestFocusInput: ctrl+b from ModeMessages switches to ModeInput.
func TestFocusInput(t *testing.T) {
	m := NewModel()
	m = initModel(t, m)

	// Put model in ModeMessages manually.
	m.mode = ModeMessages

	// Send ctrl+b.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = mustModel(t, updated)

	if m.mode != ModeInput {
		t.Errorf("expected ModeInput after ctrl+b, got %v", m.mode)
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

	line := renderMessageLine(post, "alice", "general", "Hello everyone, how are you doing today?", 120)

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

	line := renderMessageLine(post, "bob", "general", "", 120)

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

	line := renderMessageLine(post, "charlie", "random", "", 120)

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
	m := NewModelWithHeader(HeaderInfo{}, "", nil, nil, nil, nil, client, teamID, 22, false, "15")
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

	line := renderMessageLine(post, "dave", "chan", "", 40)

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
	m := NewModelWithHeader(HeaderInfo{}, "", nil, nil, nil, nil, nil, "", 22, false, "15")
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
