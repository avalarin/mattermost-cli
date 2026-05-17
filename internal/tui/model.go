package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
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
	msgCache      map[string]string // messageID -> text (for parent snippet)
	feedMessages  []feedMessage
	atBottom      bool
}

// NewModel creates a new Model with default settings.
func NewModel() Model {
	ti := textinput.New()
	ti.Placeholder = "Type / for commands..."
	ti.Prompt = "❯ "
	ti.CharLimit = 500

	return Model{
		input:    ti,
		keys:     DefaultKeyMap(),
		styles:   DefaultStyles(),
		atBottom: true,
		statusMsg: "Use /send #channel <text> to post · /send @user <text> for DMs · /quit to exit",
	}
}

// NewModelWithHeader creates a Model with pre-loaded header info, initial status,
// and optional WebSocket channels and channel list.
func NewModelWithHeader(
	header HeaderInfo,
	status string,
	events <-chan mattermost.Event,
	connStatus <-chan mattermost.ConnStatus,
	channels []mattermost.Channel,
) Model {
	m := NewModel()
	m.header = header
	if status != "" {
		m.statusMsg = status
	}
	if status != "" {
		m.statusIsError = true
	}
	m.events = events
	m.connStatus = connStatus

	// Build channelID -> channelName lookup map.
	if len(channels) > 0 {
		m.channels = make(map[string]string, len(channels))
		for _, ch := range channels {
			m.channels[ch.ID] = ch.Name
		}
	}
	m.msgCache = make(map[string]string)

	return m
}

// Init implements tea.Model. Starts waiting for the first WS event and status update.
func (m Model) Init() tea.Cmd {
	if m.events == nil {
		return nil
	}
	return tea.Batch(
		waitForEvent(m.events),
		waitForStatus(m.connStatus),
	)
}

// waitForEvent blocks until an event arrives on ch and returns it as a tea.Msg.
// Returns nil when the channel is closed (ends the subscription).
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
// Returns nil when the channel is closed (ends the subscription).
func waitForStatus(ch <-chan mattermost.ConnStatus) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return nil
		}
		return MsgConnStatus{Status: s}
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
		return m, nil

	case tea.KeyEnd:
		// Snap to bottom and re-enable auto-scroll.
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
			// Position cursor at end.
			m.input.CursorEnd()
			return m, nil
		}
	}

	// In normal mode, forward scroll keys to viewport.
	if m.ready {
		prev := m.viewport.ScrollPercent()
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		// Any upward scroll disables auto-scroll.
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
		m.mode = ModeNormal
		return m, nil

	case tea.KeyEnter:
		return m.executeCommand(m.input.Value())
	}

	// Forward other key events to the text input.
	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	return m, inputCmd
}

func (m Model) executeCommand(input string) (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(input)
	if text == "/quit" {
		return m, tea.Quit
	}
	// Unknown command — show error in status bar.
	m.statusMsg = fmt.Sprintf("Unknown command: %s", text)
	m.statusIsError = true
	m.input.SetValue("")
	m.input.Blur()
	m.mode = ModeNormal
	return m, nil
}

// handlePostedEvent decodes a posted WS event and appends the message to the feed.
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

	if m.msgCache == nil {
		m.msgCache = make(map[string]string)
	}
	m.msgCache[post.ID] = post.Text

	m.feedMessages = append(m.feedMessages, feedMessage{
		post:        post,
		senderName:  senderName,
		channelName: channelName,
	})

	m = m.rerenderFeed()

	if m.atBottom {
		m.viewport.GotoBottom()
	}

	return m, nil
}

// rerenderFeed rebuilds the viewport content from stored messages at the current width.
func (m Model) rerenderFeed() Model {
	if len(m.feedMessages) == 0 {
		m.viewport.SetContent("Waiting for messages...")
		return m
	}
	parts := make([]string, 0, len(m.feedMessages))
	for _, fm := range m.feedMessages {
		parts = append(parts, renderMessageLine(fm.post, fm.senderName, fm.channelName, m.msgCache, m.width))
	}
	m.viewport.SetContent(strings.Join(parts, "\n"))
	return m
}

const bodyIndent = "  "

// renderMessageLine formats a single message for display in the feed.
// Each message renders as 2–5 lines: a header line then up to 3 indented body
// lines, plus an overflow indicator when the text exceeds 3 lines.
// Thread replies include ↩ in the header and may show a parent snippet.
func renderMessageLine(msg mattermost.Message, senderName, channelName string, msgCache map[string]string, width int) string {
	ts := time.UnixMilli(msg.CreateAt).Format("15:04")

	var headerLine string
	if msg.RootID != "" {
		snippet := ""
		if parent, ok := msgCache[msg.RootID]; ok {
			runes := []rune(parent)
			if len(runes) > 40 {
				snippet = string(runes[:40]) + "..."
			} else {
				snippet = parent
			}
			snippet = strings.ReplaceAll(snippet, "\n", " ")
		}
		if snippet != "" {
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

	// Styled parts for rendering.
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
