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

// Model is the root Bubble Tea model.
type Model struct {
	width      int
	height     int
	mode       Mode
	header     HeaderInfo
	input      textinput.Model
	viewport   viewport.Model
	statusMsg  string
	keys       KeyMap
	styles     Styles
	ready      bool
	events     <-chan mattermost.Event
	connStatus <-chan mattermost.ConnStatus
	channels   map[string]string // channelID -> channelName
	msgCache   map[string]string // messageID -> text (for parent snippet)
	feedLines  []string          // rendered message lines
	atBottom   bool
}

// NewModel creates a new Model with default settings.
func NewModel() Model {
	ti := textinput.New()
	ti.Placeholder = "Type / for commands..."
	ti.CharLimit = 500

	return Model{
		input:    ti,
		keys:     DefaultKeyMap(),
		styles:   DefaultStyles(),
		atBottom: true,
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
	m.statusMsg = status
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

	// Layout: header(1) + feed(fills) + statusbar(1) + input(1)
	feedHeight := m.height - 3
	if feedHeight < 0 {
		feedHeight = 0
	}

	if !m.ready {
		m.viewport = viewport.New(m.width, feedHeight)
		// Restore any messages that arrived before the first window size event.
		if len(m.feedLines) > 0 {
			m.viewport.SetContent(strings.Join(m.feedLines, "\n"))
		} else {
			m.viewport.SetContent("Waiting for messages...")
		}
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = feedHeight
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
		cmd := m.executeCommand(m.input.Value())
		if cmd != nil {
			return m, cmd
		}
		return m, nil
	}

	// Forward other key events to the text input.
	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	return m, inputCmd
}

func (m Model) executeCommand(input string) tea.Cmd {
	text := strings.TrimSpace(input)
	if text == "/quit" {
		return tea.Quit
	}
	// Unknown commands: show error in status bar (handled in future tasks).
	return nil
}

// handlePostedEvent decodes a posted WS event and appends a rendered line to the feed.
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

	// Cache the message text so thread replies can show a parent snippet.
	if m.msgCache == nil {
		m.msgCache = make(map[string]string)
	}
	m.msgCache[post.ID] = post.Text

	line := renderMessageLine(post, senderName, channelName, m.msgCache)
	m.feedLines = append(m.feedLines, line)

	content := strings.Join(m.feedLines, "\n")
	m.viewport.SetContent(content)

	if m.atBottom {
		m.viewport.GotoBottom()
	}

	return m, nil
}

// renderMessageLine formats a single message for display in the feed.
// Thread replies include an arrow prefix and a snippet of the parent message.
func renderMessageLine(msg mattermost.Message, senderName, channelName string, msgCache map[string]string) string {
	ts := time.UnixMilli(msg.CreateAt).Format("15:04")

	if msg.RootID != "" {
		// Thread reply — try to show a snippet of the parent message.
		snippet := ""
		if parent, ok := msgCache[msg.RootID]; ok {
			runes := []rune(parent)
			if len(runes) > 40 {
				snippet = string(runes[:40]) + "..."
			} else {
				snippet = parent
			}
		}
		if snippet != "" {
			return fmt.Sprintf("[%s] #%-20s  ↩ %s: %s  ↩ В ответ на: %s", ts, channelName, senderName, msg.Text, snippet)
		}
		return fmt.Sprintf("[%s] #%-20s  ↩ %s: %s", ts, channelName, senderName, msg.Text)
	}

	return fmt.Sprintf("[%s] #%-20s  %s: %s", ts, channelName, senderName, msg.Text)
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

	header := m.renderHeader()
	feed := m.viewport.View()
	statusBar := m.renderStatusBar()
	inputLine := m.renderInput()

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		feed,
		statusBar,
		inputLine,
	)
}

func (m Model) renderHeader() string {
	parts := []string{"mattermost-cli"}

	status := m.header.Status
	if status == "" {
		status = mattermost.ConnStatusConnecting
	}
	parts = append(parts, fmt.Sprintf("[%s]", status))

	if m.header.TeamName != "" {
		parts = append(parts, "team: "+m.header.TeamName)
	}
	if m.header.Username != "" {
		parts = append(parts, "@"+m.header.Username)
	}

	style := lipgloss.NewStyle().
		Bold(true).
		Width(m.width).
		Foreground(lipgloss.Color("205"))
	return style.Render(strings.Join(parts, "  "))
}

func (m Model) renderStatusBar() string {
	style := lipgloss.NewStyle().
		Width(m.width).
		Foreground(lipgloss.Color("241"))
	return style.Render(m.statusMsg)
}

func (m Model) renderInput() string {
	return m.input.View()
}
