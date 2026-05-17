package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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
	// ModeNormal is the default navigation mode.
	ModeNormal Mode = iota
	// ModeCommand is the command input mode (activated by "/").
	ModeCommand
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
	width         int
	height        int
	mode          Mode
	header        HeaderInfo
	input         textinput.Model
	viewport      viewport.Model
	statusMsg     string
	statusIsError bool
	keys          KeyMap
	styles        Styles
	ready         bool
	events        <-chan mattermost.Event
	connStatus    <-chan mattermost.ConnStatus
	channels      map[string]string // channelID -> channelName
	store         *store.Store
	feedItems     []feedItem
	atBottom      bool
	client        *mattermost.Client
	teamID        string
	registry      *Registry
	statusGen     int // incremented on each MsgCommandResult to guard stale MsgClearStatus
}

// NewModel creates a new Model with default settings.
func NewModel() Model {
	ti := textinput.New()
	ti.Placeholder = "Type / for commands..."
	ti.Prompt = "❯ "
	ti.CharLimit = 500

	return Model{
		input:     ti,
		keys:      DefaultKeyMap(),
		styles:    DefaultStyles(),
		atBottom:  true,
		statusMsg: "Use /send #channel <text> to post · /send @user <text> for DMs · /quit to exit",
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

	// Layout: header(1) + divider(1) + feed(fills) + divider(1) + statusbar(1) + input(1) + divider(1)
	feedHeight := m.height - 6
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

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case ModeNormal:
		return m.handleKeyNormal(msg)
	case ModeCommand:
		return m.handleKeyCommand(msg)
	}
	return m, nil
}

func (m Model) handleKeyNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		if m.input.Value() != "" {
			m.input.SetValue("")
			return m, nil
		}
		m.statusMsg = "To exit, use /quit"
		m.statusIsError = false
		return m, nil

	case tea.KeyEnd:
		if m.ready {
			m.atBottom = true
			m.viewport.GotoBottom()
		}
		return m, nil

	case tea.KeyRunes:
		if string(msg.Runes) == "/" {
			m.mode = ModeCommand
			m.input.Focus()
			m.input.SetValue("/")
			m.input.CursorEnd()
			return m, nil
		}
	}

	if m.ready {
		prev := m.viewport.ScrollPercent()
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		if m.viewport.ScrollPercent() < prev {
			m.atBottom = false
		}
		return m, cmd
	}

	return m, nil
}

func (m Model) handleKeyCommand(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeNormal
		m.input.Blur()
		m.input.SetValue("")
		return m, nil

	case tea.KeyCtrlC:
		if m.input.Value() != "" {
			m.input.SetValue("")
			m.input.Blur()
			m.mode = ModeNormal
			return m, nil
		}
		m.statusMsg = "To exit, use /quit"
		m.statusIsError = false
		m.mode = ModeNormal
		return m, nil

	case tea.KeyEnter:
		return m.executeCommand(m.input.Value())
	}

	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	return m, inputCmd
}

func (m Model) executeCommand(input string) (tea.Model, tea.Cmd) {
	// Always reset input/mode first.
	m.input.SetValue("")
	m.input.Blur()
	m.mode = ModeNormal

	if m.registry == nil {
		// Fallback for models without a registry (e.g., tests that use NewModel()).
		text := strings.TrimSpace(input)
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
		return m, nil
	}

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
// Message items re-wrap on resize; system items are inserted verbatim.
func (m Model) rerenderFeed() Model {
	if len(m.feedItems) == 0 {
		m.viewport.SetContent("Waiting for messages...")
		return m
	}
	parts := make([]string, 0, len(m.feedItems))
	for _, item := range m.feedItems {
		switch item.kind {
		case feedItemKindMessage:
			fm := item.msg
			snippet := ""
			if fm.post.RootID != "" && m.store != nil {
				snippet = m.store.GetParentSnippet(fm.post.RootID)
			}
			parts = append(parts, renderMessageLine(fm.post, fm.senderName, fm.channelName, snippet, m.width))
		case feedItemKindSystem:
			parts = append(parts, item.system)
		}
	}
	m.viewport.SetContent(strings.Join(parts, "\n"))
	return m
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
		msg = "Use /send #channel <text> to post · /send @user <text> for DMs · /quit to exit"
	}
	color := lipgloss.Color("241") // default: gray
	if m.statusIsError {
		color = lipgloss.Color("203") // light red
	}
	return lipgloss.NewStyle().
		Width(m.width).
		Foreground(color).
		Render(msg)
}

func (m Model) renderInput() string {
	return m.input.View()
}
