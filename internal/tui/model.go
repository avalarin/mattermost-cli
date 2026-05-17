package tui

import (
	"encoding/json"
	"fmt"
	"strings"

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
	store      *store.Store
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
// WebSocket channels, channel list, and an optional store for persistence.
func NewModelWithHeader(
	header HeaderInfo,
	status string,
	events <-chan mattermost.Event,
	connStatus <-chan mattermost.ConnStatus,
	channels []mattermost.Channel,
	st *store.Store,
) Model {
	m := NewModel()
	m.header = header
	m.statusMsg = status
	m.events = events
	m.connStatus = connStatus
	m.store = st

	if len(channels) > 0 {
		m.channels = make(map[string]string, len(channels))
		for _, ch := range channels {
			m.channels[ch.ID] = ch.Name
		}
	}

	return m
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
		lines, err := s.LoadRecent(100)
		return MsgHistoryLoaded{Lines: lines, Err: err}
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
			return m, nil
		}
		if m.ready {
			lines := m.store.GetLines()
			if len(lines) > 0 {
				m.viewport.SetContent(strings.Join(lines, "\n"))
			}
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

	// Layout: header(1) + feed(fills) + statusbar(1) + input(1)
	feedHeight := m.height - 3
	if feedHeight < 0 {
		feedHeight = 0
	}

	if !m.ready {
		m.viewport = viewport.New(m.width, feedHeight)
		if m.store != nil {
			if lines := m.store.GetLines(); len(lines) > 0 {
				m.viewport.SetContent(strings.Join(lines, "\n"))
			} else {
				m.viewport.SetContent("Waiting for messages...")
			}
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
		m.mode = ModeNormal
		return m, nil

	case tea.KeyEnter:
		cmd := m.executeCommand(m.input.Value())
		if cmd != nil {
			return m, cmd
		}
		return m, nil
	}

	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	return m, inputCmd
}

func (m Model) executeCommand(input string) tea.Cmd {
	text := strings.TrimSpace(input)
	if text == "/quit" {
		return tea.Quit
	}
	return nil
}

// handlePostedEvent decodes a posted WS event and updates the feed via the store.
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

	if m.store == nil {
		return m, nil
	}

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

	if m.ready {
		m.viewport.SetContent(strings.Join(m.store.GetLines(), "\n"))
		if m.atBottom {
			m.viewport.GotoBottom()
		}
	}

	return m, nil
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
