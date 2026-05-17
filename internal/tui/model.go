package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
	"github.com/avalarin/mattermost-cli/internal/store"
)

// HeaderInfo holds the data displayed in the application header.
type HeaderInfo struct {
	TeamName string
	Username string
	Status   mattermost.ConnStatus
}

// Mode represents the current input mode of the TUI.
type Mode int

const (
	// ModeInput is the default mode: textarea is focused for typing.
	ModeInput Mode = iota
	// ModeMessages is the message navigation mode: feed cursor is active.
	ModeMessages
)

const (
	minInputHeight = 1
	maxInputHeight = 8
)

// feedMessage stores raw message data for re-rendering on resize.
type feedMessage struct {
	post        mattermost.Message
	senderName  string
	channelName string
}

// feedItemKind distinguishes the two kinds of item that can appear in the feed.
type feedItemKind int

const (
	feedItemKindMessage feedItemKind = iota
	feedItemKindSystem
)

// feedItem is a union type for the feed: either a chat message or a system-generated line.
type feedItem struct {
	kind   feedItemKind
	msg    feedMessage // valid when kind == feedItemKindMessage
	system string      // valid when kind == feedItemKindSystem (pre-formatted text)
}

// Model is the root Bubble Tea model.
type Model struct {
	width          int
	height         int
	mode           Mode
	header         HeaderInfo
	input          textarea.Model
	viewport       viewport.Model
	statusMsg      string
	statusIsError  bool
	keys           KeyMap
	styles         Styles
	ready          bool
	events         <-chan mattermost.Event
	connStatus     <-chan mattermost.ConnStatus
	channels       map[string]string // channelID -> channelName
	store          *store.Store
	feedItems      []feedItem
	atBottom       bool
	client         *mattermost.Client
	teamID         string
	registry       *Registry
	statusGen      int  // incremented on each MsgCommandResult to guard stale MsgClearStatus
	selectedMsgIdx int  // index into feedItems; -1 = none selected
	msgLineOffsets []int // starting line of each feedItem in viewport (built during rerenderFeed)
	escPending     bool // true after first Esc press, waiting for second
	escGen         int  // incremented on each Esc press to invalidate stale MsgEscTimeout
	ctrlCPending   bool // true after first Ctrl+C press, waiting for second
	ctrlCGen       int  // incremented on each Ctrl+C press to invalidate stale MsgCtrlCTimeout
}

// NewModel creates a new Model with default settings.
func NewModel() Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message, or / for commands (Enter to send)..."
	ta.ShowLineNumbers = false
	ta.SetHeight(minInputHeight)
	ta.CharLimit = 4000
	// Remap InsertNewline from Enter to Alt+Enter (opt+enter for macOS) so Enter can be used to send.
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "opt+enter"))
	// Remove the cursor-line background highlight.
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.Focus() //nolint:errcheck // Focus returns a Cmd for cursor blink, safe to ignore in NewModel

	return Model{
		input:          ta,
		keys:           DefaultKeyMap(),
		styles:         DefaultStyles(),
		atBottom:       true,
		selectedMsgIdx: -1,
		statusMsg:      "",
	}
}

// NewModelWithHeader creates a Model with pre-loaded header info, initial status,
// WebSocket channels, channel list, store, REST client, and team ID.
func NewModelWithHeader(
	header HeaderInfo,
	status string,
	events <-chan mattermost.Event,
	connStatus <-chan mattermost.ConnStatus,
	channels []mattermost.Channel,
	st *store.Store,
	client *mattermost.Client,
	teamID string,
) Model {
	m := NewModel()
	m.header = header
	if status != "" {
		m.statusMsg = status
		m.statusIsError = true
	}
	m.events = events
	m.connStatus = connStatus
	m.store = st
	m.client = client
	m.teamID = teamID

	if len(channels) > 0 {
		m.channels = make(map[string]string, len(channels))
		for _, ch := range channels {
			m.channels[ch.ID] = ch.Name
		}
	}

	m.registry = buildRegistry(client, teamID)

	return m
}

// buildRegistry constructs the command registry with all built-in commands.
func buildRegistry(client *mattermost.Client, teamID string) *Registry {
	r := NewRegistry()

	r.Register(&CommandDef{
		Name:        "quit",
		Description: "Exit the application",
		Execute:     func(_ map[string]string) tea.Cmd { return tea.Quit },
	})

	r.Register(&CommandDef{
		Name:        "send",
		Description: "Send a message to a channel or user",
		Args: []ArgSpec{
			{Name: "target", Description: "#channel or @username", Required: true},
			{Name: "text", Description: "message text", Required: true, Greedy: true},
		},
		Execute: makeSendCmd(client, teamID),
	})

	r.Register(&CommandDef{
		Name:        "help",
		Description: "Show available commands",
		Args: []ArgSpec{
			{Name: "command", Description: "command name (optional)", Required: false},
		},
		Execute: makeHelpCmd(r),
	})

	return r
}

// makeSendCmd returns the Execute function for the /send command.
func makeSendCmd(client *mattermost.Client, teamID string) func(map[string]string) tea.Cmd {
	return func(args map[string]string) tea.Cmd {
		return func() tea.Msg {
			if client == nil {
				return MsgCommandResult{Err: errors.New("not connected")}
			}
			target, text := args["target"], args["text"]
			var channelID string
			switch {
			case strings.HasPrefix(target, "#"):
				ch, err := client.GetChannelByName(teamID, strings.TrimPrefix(target, "#"))
				if errors.Is(err, mattermost.ErrChannelNotFound) {
					return MsgCommandResult{Err: fmt.Errorf("channel not found: %s", strings.TrimPrefix(target, "#"))}
				}
				if err != nil {
					return MsgCommandResult{Err: err}
				}
				channelID = ch.ID
			case strings.HasPrefix(target, "@"):
				username := strings.TrimPrefix(target, "@")
				user, err := client.GetUserByUsername(username)
				if err != nil {
					return MsgCommandResult{Err: err}
				}
				ch, err := client.FindOrCreateDM(teamID, user.ID)
				if err != nil {
					return MsgCommandResult{Err: err}
				}
				channelID = ch.ID
			default:
				return MsgCommandResult{Err: errors.New("target must start with # (channel) or @ (user)")}
			}
			if _, err := client.SendMessage(channelID, text, ""); err != nil {
				return MsgCommandResult{Err: err}
			}
			return MsgCommandResult{Info: "Sent ✓"}
		}
	}
}

// makeHelpCmd returns the Execute function for the /help command.
func makeHelpCmd(r *Registry) func(map[string]string) tea.Cmd {
	return func(args map[string]string) tea.Cmd {
		return func() tea.Msg {
			return MsgSystemMessage{Text: r.HelpText(args["command"])}
		}
	}
}

// clearStatusAfter returns a tea.Cmd that sends MsgClearStatus{Gen: gen} after d.
// The generation token prevents a stale timer from clearing a newer command's status.
func clearStatusAfter(d time.Duration, gen int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return MsgClearStatus{Gen: gen}
	}
}

// Init implements tea.Model. Starts WS event/status loops and a one-shot history load.
func (m Model) Init() tea.Cmd {
	if m.events == nil {
		return nil
	}
	cmds := []tea.Cmd{
		waitForEvent(m.events),
		waitForStatus(m.connStatus),
	}
	if m.store != nil {
		cmds = append(cmds, loadHistory(m.store))
	}
	return tea.Batch(cmds...)
}

// waitForEvent blocks until an event arrives on ch and returns it as a tea.Msg.
func waitForEvent(ch <-chan mattermost.Event) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return nil
		}
		return evt
	}
}

// waitForStatus blocks until a status arrives on ch and returns it wrapped in MsgConnStatus.
func waitForStatus(ch <-chan mattermost.ConnStatus) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return nil
		}
		return MsgConnStatus{Status: s}
	}
}

// loadHistory is a one-shot tea.Cmd that loads startup history from the store.
func loadHistory(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		msgs, err := s.LoadRecent(100)
		return MsgHistoryLoaded{Messages: msgs, Err: err}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case mattermost.Event:
		if msg.Type == mattermost.EventTypePosted {
			updated, cmd := m.handlePostedEvent(msg)
			return updated, tea.Batch(cmd, waitForEvent(m.events))
		}
		return m, waitForEvent(m.events)

	case MsgConnStatus:
		m.header.Status = msg.Status
		return m, waitForStatus(m.connStatus)

	case MsgHistoryLoaded:
		if msg.Err != nil {
			m.statusMsg = "Failed to load history: " + msg.Err.Error()
			m.statusIsError = true
			return m, nil
		}
		for _, sm := range msg.Messages {
			m.feedItems = append(m.feedItems, feedItem{
				kind: feedItemKindMessage,
				msg: feedMessage{
					post: mattermost.Message{
						ID:        sm.ID,
						ChannelID: sm.ChannelID,
						UserID:    sm.UserID,
						Text:      sm.Text,
						CreateAt:  sm.CreateAt,
						RootID:    sm.RootID,
					},
					senderName:  sm.SenderName,
					channelName: sm.ChannelName,
				},
			})
		}
		if m.ready {
			m = m.rerenderFeed()
			if m.atBottom {
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case MsgCommandResult:
		m.statusGen++
		if msg.Err != nil {
			m.statusMsg = msg.Err.Error()
			m.statusIsError = true
		} else {
			m.statusMsg = msg.Info
			m.statusIsError = false
		}
		return m, clearStatusAfter(2*time.Second, m.statusGen)

	case MsgClearStatus:
		if msg.Gen == m.statusGen {
			m.statusMsg = ""
			m.statusIsError = false
		}
		return m, nil

	case MsgSystemMessage:
		m.feedItems = append(m.feedItems, feedItem{kind: feedItemKindSystem, system: msg.Text})
		if m.ready {
			m = m.rerenderFeed()
			if m.atBottom {
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case MsgEscTimeout:
		if msg.Gen == m.escGen && m.escPending {
			m.escPending = false
			m.statusMsg = ""
			m.statusIsError = false
		}
		return m, nil

	case MsgCtrlCTimeout:
		if msg.Gen == m.ctrlCGen && m.ctrlCPending {
			m.ctrlCPending = false
			m.statusMsg = ""
			m.statusIsError = false
		}
		return m, nil
	}

	// Forward other messages to viewport when ready.
	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.input.SetWidth(m.width)

	// Input height is dynamic (1–8 lines); recompute after width change (which may reflow lines).
	inputH := m.input.LineCount()
	if inputH < minInputHeight {
		inputH = minInputHeight
	}
	if inputH > maxInputHeight {
		inputH = maxInputHeight
	}
	m.input.SetHeight(inputH)

	// Layout: header(1) + divider(1) + feed(fills) + divider(1) + statusbar(1) + input(inputH) + divider(1)
	feedHeight := m.height - 5 - inputH
	if feedHeight < 0 {
		feedHeight = 0
	}

	if !m.ready {
		m.viewport = viewport.New(m.width, feedHeight)
		m = m.rerenderFeed()
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = feedHeight
		m = m.rerenderFeed()
	}

	return m, nil
}

// syncInputHeight adjusts the textarea display height and the viewport height to match
// the current textarea content (clamped to minInputHeight–maxInputHeight lines).
// Must be called after any operation that may change the textarea content line count.
func (m Model) syncInputHeight() Model {
	lines := m.input.LineCount()
	if lines < minInputHeight {
		lines = minInputHeight
	}
	if lines > maxInputHeight {
		lines = maxInputHeight
	}
	m.input.SetHeight(lines)
	if m.ready && m.height > 0 {
		feedHeight := m.height - 5 - lines
		if feedHeight < 0 {
			feedHeight = 0
		}
		m.viewport.Height = feedHeight
	}
	return m
}

// pageSize returns the number of messages to skip per PageUp/PageDown in ModeMessages.
// Approximates how many messages fit on screen (viewport height / ~3 lines per message).
func (m Model) pageSize() int {
	n := m.viewport.Height / 3
	if n < 1 {
		n = 1
	}
	if n > 20 {
		n = 20
	}
	return n
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C is handled globally before mode dispatch — guaranteed exit path.
	if key.Matches(msg, m.keys.CtrlC) {
		return m.handleCtrlC()
	}
	switch m.mode {
	case ModeInput:
		return m.handleKeyInput(msg)
	case ModeMessages:
		return m.handleKeyMessages(msg)
	}
	return m, nil
}

// handleCtrlC implements the double-Ctrl+C exit mechanic.
// First press: show hint and start 3s window.
// Second press within 3s: quit.
func (m Model) handleCtrlC() (tea.Model, tea.Cmd) {
	if m.ctrlCPending {
		return m, tea.Quit
	}
	m.ctrlCPending = true
	m.ctrlCGen++
	gen := m.ctrlCGen
	// Invalidate any pending clearStatusAfter so the hint isn't wiped by a stale timer.
	m.statusGen++
	m.statusMsg = "Press Ctrl+C again to exit"
	m.statusIsError = false
	return m, func() tea.Msg {
		time.Sleep(3 * time.Second)
		return MsgCtrlCTimeout{Gen: gen}
	}
}

func (m Model) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Send): // enter → send/execute
		return m.executeCommand(m.input.Value())

	case key.Matches(msg, m.keys.Cancel): // esc
		return m.handleEsc()

	case key.Matches(msg, m.keys.FocusMessages): // ctrl+j → focus messages
		m.mode = ModeMessages
		m = m.selectLastMessage()
		if m.ready {
			m = m.rerenderFeed()
		}
		return m, nil

	case key.Matches(msg, m.keys.Up): // up from empty input → go to messages
		if m.input.Value() == "" {
			m.mode = ModeMessages
			m = m.selectLastMessage()
			if m.ready {
				m = m.rerenderFeed()
			}
			return m, nil
		}
		// non-empty input: fall through to textarea
	}

	// All other keys (including alt+enter which inserts a newline via textarea.KeyMap.InsertNewline).
	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	m = m.syncInputHeight()
	return m, inputCmd
}

func (m Model) handleKeyMessages(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Cancel): // esc → back to input; counts as first double-esc press
		m.mode = ModeInput
		m.input.Focus() //nolint:errcheck
		// Count this Esc as the first press of the double-esc mechanic so that
		// one more Esc in ModeInput clears the input and deselects the message.
		if !m.escPending {
			m.escPending = true
			m.escGen++
			gen := m.escGen
			m.statusMsg = "Press Esc again to clear input and deselect"
			m.statusIsError = false
			return m, func() tea.Msg {
				time.Sleep(3 * time.Second)
				return MsgEscTimeout{Gen: gen}
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.FocusInput): // ctrl+b → back to input
		m.mode = ModeInput
		m.input.Focus() //nolint:errcheck
		return m, nil

	case key.Matches(msg, m.keys.Up):
		m = m.moveCursorUp()
		if m.ready {
			m = m.rerenderFeed()
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		m = m.moveCursorDown()
		if m.ready {
			m = m.rerenderFeed()
		}
		return m, nil

	case key.Matches(msg, m.keys.End):
		m.atBottom = true
		m.viewport.GotoBottom()
		m = m.selectLastMessage()
		if m.ready {
			m = m.rerenderFeed()
		}
		return m, nil

	case key.Matches(msg, m.keys.PageUp):
		m.atBottom = false
		n := m.pageSize()
		for i := 0; i < n; i++ {
			m = m.moveCursorUp()
		}
		if m.ready {
			m = m.rerenderFeed()
		}
		return m, nil

	case key.Matches(msg, m.keys.PageDown):
		n := m.pageSize()
		for i := 0; i < n; i++ {
			m = m.moveCursorDown()
		}
		if m.ready {
			m = m.rerenderFeed()
		}
		return m, nil
	}

	// Typing "/" from messages: insert into input and go to input mode.
	// The selected message remains highlighted.
	if msg.Type == tea.KeyRunes && string(msg.Runes) == "/" {
		m.mode = ModeInput
		m.input.Focus() //nolint:errcheck
		if m.input.Value() == "" {
			m.input.SetValue("/")
		}
		return m, nil
	}

	// Forward unknown keys to viewport.
	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

// handleEsc implements the double-esc mechanic.
// First press: show a hint in the status bar and schedule a timeout.
// Second press (within 3s): clear input and deselect message.
func (m Model) handleEsc() (tea.Model, tea.Cmd) {
	if m.escPending {
		// Second esc: clear input, deselect, and scroll to bottom.
		m.input.Reset()
		m = m.syncInputHeight()
		m.selectedMsgIdx = -1
		m.escPending = false
		m.escGen++
		m.statusMsg = ""
		m.statusIsError = false
		if m.ready {
			m = m.rerenderFeed()
			m.atBottom = true
			m.viewport.GotoBottom()
		}
		return m, nil
	}
	// First esc: show hint and schedule timeout.
	m.escPending = true
	m.escGen++
	gen := m.escGen
	m.statusMsg = "Press Esc again to clear input and deselect"
	m.statusIsError = false
	return m, func() tea.Msg {
		time.Sleep(3 * time.Second)
		return MsgEscTimeout{Gen: gen}
	}
}

// selectLastMessage selects the last feedItemKindMessage in feedItems.
func (m Model) selectLastMessage() Model {
	for i := len(m.feedItems) - 1; i >= 0; i-- {
		if m.feedItems[i].kind == feedItemKindMessage {
			m.selectedMsgIdx = i
			m = m.scrollToSelected()
			return m
		}
	}
	m.selectedMsgIdx = -1
	return m
}

// moveCursorUp moves selection to the previous message.
func (m Model) moveCursorUp() Model {
	for i := m.selectedMsgIdx - 1; i >= 0; i-- {
		if m.feedItems[i].kind == feedItemKindMessage {
			m.selectedMsgIdx = i
			m.atBottom = false
			return m.scrollToSelected()
		}
	}
	return m // already at top
}

// moveCursorDown moves selection to the next message.
func (m Model) moveCursorDown() Model {
	for i := m.selectedMsgIdx + 1; i < len(m.feedItems); i++ {
		if m.feedItems[i].kind == feedItemKindMessage {
			m.selectedMsgIdx = i
			// If this is the last selectable message, scroll to bottom.
			if !m.hasSelectableMessageAfter(i) {
				m.atBottom = true
				m.viewport.GotoBottom()
			}
			return m.scrollToSelected()
		}
	}
	return m // already at bottom
}

// hasSelectableMessageAfter returns true if there is a feedItemKindMessage after index i.
func (m Model) hasSelectableMessageAfter(i int) bool {
	for j := i + 1; j < len(m.feedItems); j++ {
		if m.feedItems[j].kind == feedItemKindMessage {
			return true
		}
	}
	return false
}

// scrollToSelected scrolls the viewport to show the selected message.
func (m Model) scrollToSelected() Model {
	if !m.ready {
		return m
	}
	if m.selectedMsgIdx < 0 || m.selectedMsgIdx >= len(m.msgLineOffsets) {
		return m
	}
	lineOffset := m.msgLineOffsets[m.selectedMsgIdx]
	m.viewport.SetYOffset(lineOffset)
	return m
}

func (m Model) executeCommand(input string) (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(input)

	// Reset textarea and go to input mode.
	m.input.Reset()
	m = m.syncInputHeight()
	m.mode = ModeInput

	if m.registry == nil {
		// Fallback for models without a registry (e.g., tests that use NewModel()).
		if text == "/quit" {
			return m, tea.Quit
		}
		parts := strings.Fields(text)
		name := ""
		if len(parts) > 0 {
			name = strings.TrimPrefix(parts[0], "/")
		}
		m.statusMsg = fmt.Sprintf("Unknown command: %s", name)
		m.statusIsError = true
		m.statusGen++
		return m, clearStatusAfter(5*time.Second, m.statusGen)
	}

	if text == "" {
		return m, nil
	}

	result, err := m.registry.Parse(input)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownCommand):
			parts := strings.Fields(strings.TrimSpace(input))
			name := ""
			if len(parts) > 0 {
				name = strings.TrimPrefix(parts[0], "/")
			}
			m.statusMsg = fmt.Sprintf("Unknown command: %s", name)
		default:
			m.statusMsg = err.Error()
		}
		m.statusIsError = true
		m.statusGen++
		return m, clearStatusAfter(5*time.Second, m.statusGen)
	}

	// Valid command: clear any stale error immediately so the status bar is fresh.
	m.statusMsg = ""
	m.statusIsError = false
	return m, result.Def.Execute(result.Args)
}

// handlePostedEvent decodes a posted WS event, persists it via the store,
// and appends it to the feed for display.
func (m Model) handlePostedEvent(evt mattermost.Event) (Model, tea.Cmd) {
	postJSON, ok := evt.Data["post"].(string)
	if !ok {
		return m, nil
	}
	var post mattermost.Message
	if err := json.Unmarshal([]byte(postJSON), &post); err != nil {
		return m, nil
	}

	senderName := ""
	if v, ok := evt.Data["sender_name"].(string); ok {
		senderName = strings.TrimPrefix(v, "@")
	}
	if senderName == "" {
		senderName = post.UserID
	}

	channelName := post.ChannelID
	if name, ok := m.channels[post.ChannelID]; ok {
		channelName = name
	}

	if m.store != nil {
		m.store.AddMessage(store.Message{
			ID:          post.ID,
			ChannelID:   post.ChannelID,
			UserID:      post.UserID,
			Text:        post.Text,
			CreateAt:    post.CreateAt,
			RootID:      post.RootID,
			SenderName:  senderName,
			ChannelName: channelName,
		})
	}

	m.feedItems = append(m.feedItems, feedItem{
		kind: feedItemKindMessage,
		msg: feedMessage{
			post:        post,
			senderName:  senderName,
			channelName: channelName,
		},
	})

	m = m.rerenderFeed()

	if m.atBottom {
		m.viewport.GotoBottom()
	}

	return m, nil
}

// rerenderFeed rebuilds the viewport content from stored feed items at the current width.
// It also builds msgLineOffsets for cursor navigation and applies selection highlight.
func (m Model) rerenderFeed() Model {
	if len(m.feedItems) == 0 {
		if m.ready {
			m.viewport.SetContent("Waiting for messages...")
		}
		m.msgLineOffsets = nil
		return m
	}

	parts := make([]string, 0, len(m.feedItems))
	offsets := make([]int, len(m.feedItems))
	lineCount := 0

	for idx, item := range m.feedItems {
		offsets[idx] = lineCount
		var rendered string
		switch item.kind {
		case feedItemKindMessage:
			fm := item.msg
			snippet := ""
			if fm.post.RootID != "" && m.store != nil {
				snippet = m.store.GetParentSnippet(fm.post.RootID)
			}
			rendered = renderMessageLine(fm.post, fm.senderName, fm.channelName, snippet, m.width)
			// Apply selection highlight when this message is selected.
			if idx == m.selectedMsgIdx {
				rendered = highlightBlock(rendered, m.width)
			}
		case feedItemKindSystem:
			rendered = item.system
		}
		parts = append(parts, rendered)
		lineCount += strings.Count(rendered, "\n") + 1
	}

	m.msgLineOffsets = offsets
	if m.ready {
		m.viewport.SetContent(strings.Join(parts, "\n"))
	}
	return m
}

// highlightBlock applies a dark-gray background to all lines of a multi-line string.
func highlightBlock(s string, width int) string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color("237")).
		Width(width)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = style.Render(l)
	}
	return strings.Join(lines, "\n")
}

const bodyIndent = "  "

// renderMessageLine formats a single message for display in the feed.
// Each message renders as 2–5 lines: a header line then up to 3 indented body
// lines, plus an overflow indicator when the text exceeds 3 lines.
// Thread replies include ↩ in the header and may show a parent snippet.
func renderMessageLine(msg mattermost.Message, senderName, channelName, snippet string, width int) string {
	ts := time.UnixMilli(msg.CreateAt).Format("15:04")

	var headerLine string
	if msg.RootID != "" {
		if snippet != "" {
			snippet = strings.ReplaceAll(snippet, "\n", " ")
			headerLine = fmt.Sprintf("[%s] #%s  ↩ @%s  (%s)", ts, channelName, senderName, snippet)
		} else {
			headerLine = fmt.Sprintf("[%s] #%s  ↩ @%s", ts, channelName, senderName)
		}
	} else {
		headerLine = fmt.Sprintf("[%s] #%s  @%s", ts, channelName, senderName)
	}

	// Word-wrap the body, accounting for indent, and cap at 3 visible lines.
	wrapWidth := width - len([]rune(bodyIndent))
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	if width <= 0 {
		wrapWidth = 120
	}
	bodyLines := wrapText(msg.Text, wrapWidth)

	overflowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("238")).
		Italic(true)

	allLines := []string{headerLine}
	if len(bodyLines) <= 3 {
		for _, l := range bodyLines {
			allLines = append(allLines, bodyIndent+l)
		}
	} else {
		for _, l := range bodyLines[:3] {
			allLines = append(allLines, bodyIndent+l)
		}
		remaining := len(bodyLines) - 3
		indicator := overflowStyle.Render(fmt.Sprintf("⌄⌄⌄  %d more lines", remaining))
		allLines = append(allLines, bodyIndent+indicator)
	}

	return strings.Join(allLines, "\n")
}

// wrapText splits text into lines of at most width runes, breaking on word boundaries.
// It also preserves existing newlines in the source text.
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var result []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			continue
		}
		var line strings.Builder
		lineWidth := 0
		for _, word := range words {
			wordWidth := len([]rune(word))
			if lineWidth == 0 {
				line.WriteString(word)
				lineWidth = wordWidth
			} else if lineWidth+1+wordWidth <= width {
				line.WriteByte(' ')
				line.WriteString(word)
				lineWidth += 1 + wordWidth
			} else {
				result = append(result, line.String())
				line.Reset()
				line.WriteString(word)
				lineWidth = wordWidth
			}
		}
		if lineWidth > 0 {
			result = append(result, line.String())
		}
	}
	return result
}

// StatusMsg returns the current status bar message (for testing).
func (m Model) StatusMsg() string {
	return m.statusMsg
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// strings.Join is used instead of lipgloss.JoinVertical to avoid implicit
	// width normalization that can interact badly with pre-styled strings.
	return strings.Join([]string{
		m.renderHeader(),
		m.renderDivider(),
		m.viewport.View(),
		m.renderDivider(),
		m.renderStatusBar(),
		m.renderInput(),
		m.renderDivider(),
	}, "\n")
}

func (m Model) renderHeader() string {
	isConnected := m.header.Status == mattermost.ConnStatusConnected || m.header.Status == ""
	dotColor := lipgloss.Color("10") // bright green
	if !isConnected {
		dotColor = lipgloss.Color("11") // bright yellow
	}

	styledDot := lipgloss.NewStyle().Foreground(dotColor).Render("●")
	styledApp := lipgloss.NewStyle().Bold(true).Render(" mattermost")

	rightPlain := ""
	styledRight := ""
	if m.header.Username != "" {
		rightPlain = "@" + m.header.Username
		styledRight = lipgloss.NewStyle().Foreground(lipgloss.Color("247")).Render(rightPlain)
	}

	if m.width <= 0 {
		return styledDot + styledApp + "  " + styledRight
	}

	// Compute gap from plain-text rune widths to avoid ANSI miscounting.
	leftPlain := "● mattermost"
	gap := m.width - len([]rune(leftPlain)) - len([]rune(rightPlain))
	if gap < 1 {
		gap = 1
	}
	return styledDot + styledApp + strings.Repeat(" ", gap) + styledRight
}

func (m Model) renderDivider() string {
	if m.width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", m.width))
}

func (m Model) renderStatusBar() string {
	msg := m.statusMsg
	if msg == "" {
		if m.selectedMsgIdx >= 0 && m.selectedMsgIdx < len(m.feedItems) {
			item := m.feedItems[m.selectedMsgIdx]
			if item.kind == feedItemKindMessage {
				msgID := item.msg.post.ID
				if len(msgID) > 8 {
					msgID = msgID[:8] + "..."
				}
				msg = fmt.Sprintf("selected: %s  (esc twice to unselect)", msgID)
			}
		} else {
			msg = "Enter to send · Alt/Opt+Enter for newline · /send #channel · /quit to exit"
		}
	}
	color := lipgloss.Color("241")
	if m.statusIsError {
		color = lipgloss.Color("203")
	}
	return lipgloss.NewStyle().Width(m.width).Foreground(color).Render(msg)
}

func (m Model) renderInput() string {
	return m.input.View()
}
