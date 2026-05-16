package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConnStatus represents the WebSocket connection state shown in the header.
type ConnStatus string

const (
	ConnStatusConnecting ConnStatus = "connecting"
	ConnStatusConnected  ConnStatus = "connected"
)

// HeaderInfo holds the data displayed in the application header.
type HeaderInfo struct {
	TeamName string
	Username string
	Status   ConnStatus
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
	width     int
	height    int
	mode      Mode
	header    HeaderInfo
	input     textinput.Model
	viewport  viewport.Model
	statusMsg string
	keys      KeyMap
	styles    Styles
	ready     bool
}

// NewModel creates a new Model with default settings.
func NewModel() Model {
	ti := textinput.New()
	ti.Placeholder = "Type / for commands..."
	ti.CharLimit = 500

	return Model{
		input:  ti,
		keys:   DefaultKeyMap(),
		styles: DefaultStyles(),
	}
}

// NewModelWithHeader creates a Model with pre-loaded header info and initial status.
func NewModelWithHeader(header HeaderInfo, status string) Model {
	m := NewModel()
	m.header = header
	m.statusMsg = status
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
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
		m.viewport.SetContent("Waiting for messages... (no config loaded)")
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
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
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
		status = ConnStatusConnecting
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
