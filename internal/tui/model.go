package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
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
	// ModeHelp is the help popup mode: scrollable keyboard shortcut reference.
	ModeHelp
	// ModeChannels is the channels sidebar navigation mode.
	ModeChannels
)

const (
	minInputHeight = 1
	maxInputHeight = 8
)

// Model is the root Bubble Tea model.
type Model struct {
	width         int
	height        int
	mode          Mode
	header        HeaderInfo
	input         textarea.Model
	statusMsg     string
	statusIsError bool
	keys          KeyMap
	styles        Styles
	ready         bool
	events        <-chan mattermost.Event
	connStatus    <-chan mattermost.ConnStatus
	channels      map[string]string // channelID -> channelName
	store         *store.Store
	client        *mattermost.Client
	teamID        string
	registry      *Registry
	statusGen     int  // incremented on each MsgCommandResult to guard stale MsgClearStatus
	escPending    bool // true after first Esc press, waiting for second
	escGen        int  // incremented on each Esc press to invalidate stale MsgEscTimeout
	ctrlCPending  bool // true after first Ctrl+C press, waiting for second
	ctrlCGen      int  // incremented on each Ctrl+C press to invalidate stale MsgCtrlCTimeout
	prefixPending bool // true after Ctrl+B press, waiting for arrow key
	prefixGen     int  // incremented on each Ctrl+B press to invalidate stale MsgPrefixTimeout
	prevMode      Mode           // mode before help popup opened, restored on close
	helpViewport  viewport.Model // scrollable popup content
	helpReady     bool           // whether helpViewport has been initialized

	// Two-panel layout.
	messagesView    MessagesView
	channelsView    ChannelsView
	activeChannelID string // "" = All Activity
	channelsWidth   int    // from config, default 22
	feedH           int    // cached body height, updated in handleWindowSize/syncInputHeight

	channelsRaw       []mattermost.Channel    // original channel list, used for DM name resolution
	channelTypes      map[string]string       // channelID → type ("O","P","D","G")
	showModeIndicator bool                    // when true, status bar shows current mode badge
	activeHeaderFg    string                  // foreground color for the active panel header
	activeHeaderBg    string                  // background color for the active panel header
	fullDateFormat    string                  // Go time layout for dates outside today

	activePage      map[string]int // channelID → next page to load (for infinite scroll)
	historyLoading  bool           // true while a channel history fetch is in flight
	channelMessages string         // "root_only" | "all" — controls reply visibility in channel view
	spinner         spinner.Model  // animated MiniDot shown while channel history loads
}

// clamp returns v clamped to [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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
	// Use a vertical bar as the prompt so it acts as a left focus indicator.
	ta.Prompt = "❯ "
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	ta.Focus() //nolint:errcheck // Focus returns a Cmd for cursor blink, safe to ignore in NewModel

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot

	return Model{
		input:         ta,
		keys:          DefaultKeyMap(),
		styles:        DefaultStyles(),
		statusMsg:     "",
		channelsWidth: 22,
		messagesView:  NewMessagesView(nil),
		activePage:    make(map[string]int),
		spinner:       sp,
	}
}

// NewModelWithHeader creates a Model with pre-loaded header info, initial status,
// WebSocket channels, channel list, store, REST client, team ID, channels sidebar width,
// whether to display the mode indicator in the status bar, the full-date format
// used for messages not sent today (Go time layout, e.g. "02.01.2006"), and the
// channel_messages setting ("root_only" | "all").
func NewModelWithHeader(
	header HeaderInfo,
	status string,
	events <-chan mattermost.Event,
	connStatus <-chan mattermost.ConnStatus,
	channels []mattermost.Channel,
	st *store.Store,
	client *mattermost.Client,
	teamID string,
	channelsWidth int,
	showModeIndicator bool,
	activeHeaderFg string,
	activeHeaderBg string,
	fullDateFormat string,
	channelMessages string,
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
	m.showModeIndicator = showModeIndicator
	if activeHeaderFg != "" {
		m.activeHeaderFg = activeHeaderFg
	} else {
		m.activeHeaderFg = "15"
	}
	if activeHeaderBg != "" {
		m.activeHeaderBg = activeHeaderBg
	} else {
		m.activeHeaderBg = "237"
	}
	if fullDateFormat != "" {
		m.fullDateFormat = fullDateFormat
	} else {
		m.fullDateFormat = "02.01.2006"
	}

	if channelsWidth > 0 {
		m.channelsWidth = channelsWidth
	}

	if len(channels) > 0 {
		m.channels = make(map[string]string, len(channels))
		m.channelTypes = make(map[string]string, len(channels))
		for _, ch := range channels {
			m.channels[ch.ID] = ch.Name
			m.channelTypes[ch.ID] = ch.Type
		}
	}

	if channelMessages == "all" {
		m.channelMessages = "all"
	} else {
		m.channelMessages = "root_only"
	}

	m.channelsRaw = channels
	m.channelsView = NewChannelsView(channels)
	m.messagesView = NewMessagesView(st).SetFullDateFormat(fullDateFormat).SetAllActivity(true)
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

	r.Register(&CommandDef{
		Name:        "reload",
		Description: "Load more message history for the current channel",
		Execute: func(_ map[string]string) tea.Cmd {
			return func() tea.Msg { return MsgRequestReload{} }
		},
	})

	r.Register(&CommandDef{
		Name:        "reset",
		Description: "Clear in-memory caches; /reset db also wipes the database",
		Args: []ArgSpec{
			{Name: "target", Description: "db (optional)", Required: false},
		},
		Execute: func(args map[string]string) tea.Cmd {
			if args["target"] == "db" {
				return func() tea.Msg { return MsgResetDB{} }
			}
			return func() tea.Msg { return MsgResetCaches{} }
		},
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
// With no args it opens the help popup; with a command name it shows detail in the feed.
func makeHelpCmd(r *Registry) func(map[string]string) tea.Cmd {
	return func(args map[string]string) tea.Cmd {
		return func() tea.Msg {
			if args["command"] != "" {
				return MsgSystemMessage{Text: r.HelpText(args["command"])}
			}
			return MsgOpenHelp{}
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

// loadChannelHistoryCmd returns a tea.Cmd that fetches channel history from REST
// and batch-resolves the author usernames in a single extra API call.
// prepend=true is used for infinite scroll (older messages prepended at the top).
func loadChannelHistoryCmd(client *mattermost.Client, channelID string, page int, prepend bool) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return MsgChannelHistory{
				ChannelID: channelID,
				Err:       errors.New("not connected"),
				Prepend:   prepend,
			}
		}
		msgs, err := client.GetChannelPosts(channelID, page, 100)
		if err != nil {
			return MsgChannelHistory{ChannelID: channelID, Prepend: prepend, Err: err}
		}
		// Batch-resolve unique user IDs to display usernames.
		seen := make(map[string]bool, len(msgs))
		var ids []string
		for _, m := range msgs {
			if !seen[m.UserID] {
				seen[m.UserID] = true
				ids = append(ids, m.UserID)
			}
		}
		var userNames map[string]string
		if len(ids) > 0 {
			if users, uerr := client.GetUsersByIDs(ids); uerr == nil {
				userNames = make(map[string]string, len(users))
				for id, u := range users {
					userNames[id] = u.Username
				}
			}
		}
		return MsgChannelHistory{
			ChannelID: channelID,
			Messages:  msgs,
			Prepend:   prepend,
			UserNames: userNames,
		}
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
	if m.client != nil {
		cmds = append(cmds, resolveDMNames(m.client, m.channelsRaw))
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

// resolveDMNames fetches usernames for DM channels and returns MsgDMNamesResolved.
// For each DM channel (Type == "D"), it parses the channel name (userID1__userID2),
// determines the other user, fetches their profile, and maps channelID -> "@username".
func resolveDMNames(client *mattermost.Client, channels []mattermost.Channel) tea.Cmd {
	return func() tea.Msg {
		selfID := client.CurrentUserID()
		// Collect unique other-user IDs from DM channels.
		var ids []string
		seen := make(map[string]bool)
		dmChannelIDs := make(map[string]string) // other userID → channel ID
		for _, ch := range channels {
			if ch.Type != "D" {
				continue
			}
			// name format: userID1__userID2
			parts := strings.SplitN(ch.Name, "__", 2)
			if len(parts) != 2 {
				continue
			}
			otherID := parts[0]
			if otherID == selfID {
				otherID = parts[1]
			}
			if !seen[otherID] {
				seen[otherID] = true
				ids = append(ids, otherID)
			}
			dmChannelIDs[otherID] = ch.ID
		}
		if len(ids) == 0 {
			return nil
		}
		users, err := client.GetUsersByIDs(ids)
		if err != nil {
			return nil // silently ignore; IDs remain as fallback
		}
		names := make(map[string]string, len(users))
		for otherID, u := range users {
			chID, ok := dmChannelIDs[otherID]
			if !ok {
				continue
			}
			name := u.Username
			if name == "" {
				name = otherID
			}
			names[chID] = name
		}
		return MsgDMNamesResolved{Names: names}
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
		// All Activity always shows all messages (no root_only filter here).
		// Per-channel root_only filtering is applied in MsgChannelHistory.
		items := make([]feedItem, 0, len(msg.Messages))
		for _, sm := range msg.Messages {
			isDM := m.channelTypes[sm.ChannelID] == "D" || m.channelTypes[sm.ChannelID] == "G"
			items = append(items, feedItem{
				kind:     feedItemKindMessage,
				createAt: sm.CreateAt,
				msg: feedMessage{
					post: mattermost.Message{
						ID:         sm.ID,
						ChannelID:  sm.ChannelID,
						UserID:     sm.UserID,
						Text:       sm.Text,
						CreateAt:   sm.CreateAt,
						RootID:     sm.RootID,
						ReplyCount: sm.ReplyCount,
					},
					senderName:  sm.SenderName,
					channelName: sm.ChannelName,
					isDM:        isDM,
				},
			})
		}
		m.messagesView = m.messagesView.SetFeedItems(items)
		if m.messagesView.AtBottom() {
			m.messagesView = m.messagesView.GotoBottom()
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
		m.messagesView = m.messagesView.AddFeedItem(feedItem{kind: feedItemKindSystem, createAt: time.Now().UnixMilli(), system: msg.Text})
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

	case MsgPrefixTimeout:
		if msg.Gen == m.prefixGen && m.prefixPending {
			m.prefixPending = false
			m.statusMsg = ""
			m.statusIsError = false
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if !m.historyLoading {
			return m, nil
		}
		m.messagesView = m.messagesView.SetFeedItems([]feedItem{
			{kind: feedItemKindSystem, system: m.spinner.View() + " Loading…"},
		})
		return m, cmd

	case MsgOpenHelp:
		return m.openHelp()

	case MsgChannelSelected:
		m.activeChannelID = msg.ChannelID
		m.channelsView = m.channelsView.SetOpenByID(msg.ChannelID)
		if msg.ChannelID == "" {
			// All Activity: rebuild feed from the global in-memory message list
			// so it reflects messages received while a specific channel was open.
			m.messagesView = m.messagesView.SetAllActivity(true)
			if m.store != nil {
				globalMsgs := m.store.GetMessages()
				items := make([]feedItem, 0, len(globalMsgs))
				for _, sm := range globalMsgs {
					isDM := m.channelTypes[sm.ChannelID] == "D" || m.channelTypes[sm.ChannelID] == "G"
					items = append(items, feedItem{
						kind:     feedItemKindMessage,
						createAt: sm.CreateAt,
						msg: feedMessage{
							post: mattermost.Message{
								ID:         sm.ID,
								ChannelID:  sm.ChannelID,
								UserID:     sm.UserID,
								Text:       sm.Text,
								CreateAt:   sm.CreateAt,
								RootID:     sm.RootID,
								ReplyCount: sm.ReplyCount,
							},
							senderName:  sm.SenderName,
							channelName: sm.ChannelName,
							isDM:        isDM,
						},
					})
				}
				m.messagesView = m.messagesView.SetFeedItems(items)
				m.messagesView = m.messagesView.GotoBottom()
			}
			return m, nil
		}
		m.messagesView = m.messagesView.SetAllActivity(false)
		// Clear stale messages immediately so the old channel's feed isn't visible
		// while the new one loads.
		m.messagesView = m.messagesView.SetFeedItems([]feedItem{
			{kind: feedItemKindSystem, system: m.spinner.View() + " Loading…"},
		})
		// Load first page of history for this channel.
		m.historyLoading = true
		m.activePage[msg.ChannelID] = 0
		channelID := msg.ChannelID
		return m, tea.Batch(
			func() tea.Msg { return MsgChannelHistoryLoading{ChannelID: channelID} },
			loadChannelHistoryCmd(m.client, channelID, 0, false),
			m.spinner.Tick,
		)

	case MsgChannelHistoryLoading:
		m.historyLoading = true
		return m, nil

	case MsgChannelHistory:
		m.historyLoading = false
		if msg.Err != nil {
			m.statusMsg = "Failed to load history: " + msg.Err.Error()
			m.statusIsError = true
			m.statusGen++
			return m, clearStatusAfter(3*time.Second, m.statusGen)
		}
		if msg.ChannelID != m.activeChannelID {
			// Channel switched while loading; discard stale result.
			return m, nil
		}
		isDM := m.channelTypes[msg.ChannelID] == "D" || m.channelTypes[msg.ChannelID] == "G"
		// Convert REST messages to store messages, using resolved usernames when available.
		storeMessages := make([]store.Message, 0, len(msg.Messages))
		for _, p := range msg.Messages {
			name := msg.UserNames[p.UserID]
			if name == "" {
				name = p.UserID
			}
			chName := m.channels[p.ChannelID]
			if chName == "" {
				chName = p.ChannelID
			}
			storeMessages = append(storeMessages, store.Message{
				ID:          p.ID,
				ChannelID:   p.ChannelID,
				UserID:      p.UserID,
				Text:        p.Text,
				CreateAt:    p.CreateAt,
				RootID:      p.RootID,
				SenderName:  name,
				ChannelName: chName,
				ReplyCount:  p.ReplyCount,
			})
		}
		if m.store != nil {
			m.store.AddChannelMessages(msg.ChannelID, storeMessages, msg.Prepend)
		}
		// Increment page counter.
		if !msg.Prepend {
			m.activePage[msg.ChannelID] = 1
		} else {
			m.activePage[msg.ChannelID]++
		}
		// Build feed items from channel cache.
		var channelMsgs []store.Message
		if m.store != nil {
			channelMsgs = m.store.GetChannelMessages(msg.ChannelID)
		} else {
			channelMsgs = storeMessages
		}
		items := make([]feedItem, 0, len(channelMsgs))
		for _, sm := range channelMsgs {
			// In root_only mode, skip replies (messages that are part of a thread).
			if m.channelMessages == "root_only" && sm.RootID != "" {
				continue
			}
			items = append(items, feedItem{
				kind:     feedItemKindMessage,
				createAt: sm.CreateAt,
				msg: feedMessage{
					post: mattermost.Message{
						ID:         sm.ID,
						ChannelID:  sm.ChannelID,
						UserID:     sm.UserID,
						Text:       sm.Text,
						CreateAt:   sm.CreateAt,
						RootID:     sm.RootID,
						ReplyCount: sm.ReplyCount,
					},
					senderName:  sm.SenderName,
					channelName: sm.ChannelName,
					isDM:        isDM,
				},
			})
		}
		m.messagesView = m.messagesView.SetFeedItems(items)
		if !msg.Prepend {
			m.messagesView = m.messagesView.GotoBottom()
		}
		return m, nil

	case MsgRequestReload:
		if m.activeChannelID == "" {
			m.statusMsg = "Not in a channel: use /reload only inside a specific channel"
			m.statusIsError = true
			m.statusGen++
			return m, clearStatusAfter(3*time.Second, m.statusGen)
		}
		page := m.activePage[m.activeChannelID]
		m.historyLoading = true
		return m, tea.Batch(
			func() tea.Msg { return MsgChannelHistoryLoading{ChannelID: m.activeChannelID} },
			loadChannelHistoryCmd(m.client, m.activeChannelID, page, true),
		)

	case MsgResetCaches:
		if m.store != nil {
			m.store.Reset()
		}
		m.messagesView = m.messagesView.SetFeedItems(nil)
		m.statusMsg = "Caches cleared"
		m.statusIsError = false
		m.statusGen++
		return m, clearStatusAfter(3*time.Second, m.statusGen)

	case MsgResetDB:
		if m.store != nil {
			m.store.Reset()
			if err := m.store.DeleteAllMessages(); err != nil {
				m.statusMsg = fmt.Sprintf("reset db: %v", err)
				m.statusIsError = true
				m.statusGen++
				return m, clearStatusAfter(5*time.Second, m.statusGen)
			}
		}
		m.messagesView = m.messagesView.SetFeedItems(nil)
		m.statusMsg = "Caches and database cleared"
		m.statusIsError = false
		m.statusGen++
		return m, clearStatusAfter(3*time.Second, m.statusGen)

	case MsgDMNamesResolved:
		m.channelsView = m.channelsView.ApplyDMNames(msg.Names)
		return m, nil
	}

	// Forward other messages to viewport when ready.
	if m.ready {
		var cmd tea.Cmd
		m.messagesView, cmd = m.messagesView.UpdateVP(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.input.SetWidth(m.width)

	inputH := clamp(m.input.LineCount(), minInputHeight, maxInputHeight)
	m.input.SetHeight(inputH)

	feedH := m.height - 5 - inputH
	if feedH < 0 {
		feedH = 0
	}
	m.feedH = feedH

	chW := m.channelsWidth
	msgsW := m.width - chW - 1 // 1 for vertical divider
	if msgsW < 1 {
		msgsW = 1
	}

	m.channelsView = m.channelsView.SetSize(chW, feedH)
	m.messagesView = m.messagesView.SetSize(msgsW, feedH)
	m.ready = true

	if m.helpReady {
		_, _, innerW, innerH := m.helpDimensions()
		m.helpViewport.Width = innerW
		m.helpViewport.Height = innerH
		m.helpViewport.SetContent(buildHelpContent(m.keys, m.registry, innerW))
	}

	return m, nil
}

// syncInputHeight adjusts the textarea display height and the panel heights to match
// the current textarea content (clamped to minInputHeight–maxInputHeight lines).
func (m Model) syncInputHeight() Model {
	lines := clamp(m.input.LineCount(), minInputHeight, maxInputHeight)
	m.input.SetHeight(lines)
	if m.ready && m.height > 0 {
		feedH := m.height - 5 - lines
		if feedH < 0 {
			feedH = 0
		}
		m.feedH = feedH
		chW := m.channelsWidth
		msgsW := m.width - chW - 1
		if msgsW < 1 {
			msgsW = 1
		}
		m.channelsView = m.channelsView.SetSize(chW, feedH)
		m.messagesView = m.messagesView.SetSize(msgsW, feedH)
	}
	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// In ModeHelp, Ctrl+C closes the popup instead of triggering the exit mechanic.
	if m.mode == ModeHelp && key.Matches(msg, m.keys.CtrlC) {
		return m.closeHelp()
	}
	// Ctrl+C is handled globally before mode dispatch — guaranteed exit path.
	if key.Matches(msg, m.keys.CtrlC) {
		return m.handleCtrlC()
	}

	// Prefix mode: Ctrl+B was pressed, waiting for an arrow key.
	// Not active in ModeHelp to keep the help popup unaffected.
	if m.mode != ModeHelp {
		if m.prefixPending {
			m.prefixPending = false
			m.statusMsg = ""
			m.statusIsError = false
			switch {
			case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.Right):
				m.mode = ModeMessages
				m.input.Blur()
				m.messagesView = m.messagesView.SelectLast()
				if m.ready {
					m.messagesView = m.messagesView.rerenderFeed()
				}
				return m, nil
			case key.Matches(msg, m.keys.Down):
				m.mode = ModeInput
				m.input.Focus() //nolint:errcheck
				return m, nil
			case key.Matches(msg, m.keys.Left):
				m.mode = ModeChannels
				m.input.Blur()
				return m, nil
			default:
				// Non-arrow key cancels prefix mode; process the key normally.
			}
		}

		// Activate prefix mode on Ctrl+B.
		if key.Matches(msg, m.keys.Prefix) {
			m.prefixPending = true
			m.prefixGen++
			gen := m.prefixGen
			m.statusGen++
			m.statusMsg = "Ctrl+B — press ↑/↓/←/→ to navigate"
			m.statusIsError = false
			return m, func() tea.Msg {
				time.Sleep(2 * time.Second)
				return MsgPrefixTimeout{Gen: gen}
			}
		}
	}

	switch m.mode {
	case ModeInput:
		return m.handleKeyInput(msg)
	case ModeMessages:
		return m.handleKeyMessages(msg)
	case ModeHelp:
		return m.handleKeyHelp(msg)
	case ModeChannels:
		return m.handleKeyChannels(msg)
	}
	return m, nil
}

// handleCtrlC implements the double-Ctrl+C exit mechanic.
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

	case key.Matches(msg, m.keys.FocusChannels): // ctrl+l → focus channels
		m.mode = ModeChannels
		m.input.Blur()
		return m, nil

	case key.Matches(msg, m.keys.FocusMessages): // ctrl+j → focus messages
		m.mode = ModeMessages
		m.input.Blur()
		m.messagesView = m.messagesView.SelectLast()
		if m.ready {
			m.messagesView = m.messagesView.rerenderFeed()
		}
		return m, nil

	case key.Matches(msg, m.keys.Up): // up from empty input → go to messages
		if m.input.Value() == "" {
			m.mode = ModeMessages
			m.input.Blur()
			m.messagesView = m.messagesView.SelectLast()
			if m.ready {
				m.messagesView = m.messagesView.rerenderFeed()
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

	case key.Matches(msg, m.keys.FocusChannels): // ctrl+l → focus channels
		m.mode = ModeChannels
		return m, nil

	case key.Matches(msg, m.keys.Up):
		m.messagesView = m.messagesView.MoveCursorUp()
		// Infinite scroll: load older messages when at the top (and view is non-empty).
		if m.messagesView.AtTop() && !m.messagesView.IsEmpty() && m.activeChannelID != "" && !m.historyLoading {
			page := m.activePage[m.activeChannelID]
			m.historyLoading = true
			return m, tea.Batch(
				func() tea.Msg { return MsgChannelHistoryLoading{ChannelID: m.activeChannelID} },
				loadChannelHistoryCmd(m.client, m.activeChannelID, page, true),
			)
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		m.messagesView = m.messagesView.MoveCursorDown()
		return m, nil

	case key.Matches(msg, m.keys.End):
		m.messagesView = m.messagesView.SelectLast()
		m.messagesView = m.messagesView.GotoBottom()
		if m.ready {
			m.messagesView = m.messagesView.rerenderFeed()
		}
		return m, nil

	case key.Matches(msg, m.keys.PageUp):
		m.messagesView = m.messagesView.SetAtBottom(false)
		n := m.messagesView.PageSize()
		for i := 0; i < n; i++ {
			m.messagesView = m.messagesView.MoveCursorUp()
		}
		// Infinite scroll: load older messages when at the top (and view is non-empty).
		if m.messagesView.AtTop() && !m.messagesView.IsEmpty() && m.activeChannelID != "" && !m.historyLoading {
			page := m.activePage[m.activeChannelID]
			m.historyLoading = true
			return m, tea.Batch(
				func() tea.Msg { return MsgChannelHistoryLoading{ChannelID: m.activeChannelID} },
				loadChannelHistoryCmd(m.client, m.activeChannelID, page, true),
			)
		}
		return m, nil

	case key.Matches(msg, m.keys.PageDown):
		n := m.messagesView.PageSize()
		for i := 0; i < n; i++ {
			m.messagesView = m.messagesView.MoveCursorDown()
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
		m.messagesView, cmd = m.messagesView.UpdateVP(msg)
		return m, cmd
	}

	return m, nil
}

// handleKeyChannels handles key input when in ModeChannels.
func (m Model) handleKeyChannels(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Cancel): // esc → input
		m.mode = ModeInput
		m.input.Focus() //nolint:errcheck
		return m, nil

	case key.Matches(msg, m.keys.FocusMessages): // ctrl+j → messages
		m.mode = ModeMessages
		m.messagesView = m.messagesView.SelectLast()
		if m.ready {
			m.messagesView = m.messagesView.rerenderFeed()
		}
		return m, nil

	case key.Matches(msg, m.keys.Up):
		m.channelsView = m.channelsView.MoveUp()
		return m, nil

	case key.Matches(msg, m.keys.Down):
		m.channelsView = m.channelsView.MoveDown()
		return m, nil

	case key.Matches(msg, m.keys.PageUp):
		m.channelsView = m.channelsView.PageUp()
		return m, nil

	case key.Matches(msg, m.keys.PageDown):
		m.channelsView = m.channelsView.PageDown()
		return m, nil

	case key.Matches(msg, m.keys.Send): // enter → open channel
		if m.channelsView.IsSelectedArchived() {
			name := m.channelsView.SelectedDisplayName()
			m.statusMsg = "Channel " + name + " is archived"
			m.statusIsError = true
			m.statusGen++
			return m, clearStatusAfter(3*time.Second, m.statusGen)
		}
		var channelID string
		m.channelsView, channelID = m.channelsView.OpenSelected()
		m.activeChannelID = channelID
		return m, func() tea.Msg { return MsgChannelSelected{ChannelID: channelID} }
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
		m.messagesView = m.messagesView.ClearSelection()
		m.escPending = false
		m.escGen++
		m.statusMsg = ""
		m.statusIsError = false
		if m.ready {
			m.messagesView = m.messagesView.GotoBottom()
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

	sm := store.Message{
		ID:          post.ID,
		ChannelID:   post.ChannelID,
		UserID:      post.UserID,
		Text:        post.Text,
		CreateAt:    post.CreateAt,
		RootID:      post.RootID,
		SenderName:  senderName,
		ChannelName: channelName,
		ReplyCount:  post.ReplyCount, // always 0 for a fresh post; incremented separately via IncrementReplyCount
	}

	if m.store != nil {
		m.store.AddMessage(sm)
		// Also add to the per-channel cache.
		m.store.AddChannelMessages(post.ChannelID, []store.Message{sm}, false)
		if post.RootID != "" {
			m.store.IncrementReplyCount(post.RootID)
		}
	}
	// Update the reply count badge in the view regardless of whether the store is present.
	if post.RootID != "" {
		m.messagesView = m.messagesView.IncrementReplyCount(post.RootID)
	}

	isDM := m.channelTypes[post.ChannelID] == "D" || m.channelTypes[post.ChannelID] == "G"
	// Only add to messages view if: All Activity OR message is in active channel.
	shouldAdd := m.activeChannelID == "" || post.ChannelID == m.activeChannelID
	// In root_only channel mode, skip replies in the feed (they only bump the parent badge).
	if shouldAdd && m.activeChannelID != "" && m.channelMessages == "root_only" && post.RootID != "" {
		shouldAdd = false
	}
	if shouldAdd {
		m.messagesView = m.messagesView.AddFeedItem(feedItem{
			kind:     feedItemKindMessage,
			createAt: post.CreateAt,
			msg: feedMessage{
				post:        post,
				senderName:  senderName,
				channelName: channelName,
				isDM:        isDM,
			},
		})
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

	if m.mode == ModeHelp {
		return m.renderHelp()
	}

	body := m.renderBody()
	return strings.Join([]string{
		m.renderHeader(),
		m.renderDivider(),
		body,
		m.renderDivider(),
		m.renderStatusBar(),
		m.renderInput(),
		m.renderDivider(),
	}, "\n")
}

// renderBody renders the two-panel body: channels sidebar + vertical divider + messages panel.
func (m Model) renderBody() string {
	channelsPanel := m.channelsView.SetActive(m.mode == ModeChannels).SetActiveFg(m.activeHeaderFg).SetActiveBg(m.activeHeaderBg).View()
	msgsHeader := m.renderMessagesHeader()
	msgsContent := m.messagesView.View()

	// Build the messages panel: header + viewport content.
	msgsPanel := msgsHeader + "\n" + msgsContent

	chLines := strings.Split(channelsPanel, "\n")
	msLines := strings.Split(msgsPanel, "\n")

	divChar := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render("│")

	bodyLines := make([]string, m.feedH)
	for i := range bodyLines {
		cl := ""
		if i < len(chLines) {
			cl = chLines[i]
		}
		ml := ""
		if i < len(msLines) {
			ml = msLines[i]
		}
		bodyLines[i] = cl + divChar + ml
	}
	return strings.Join(bodyLines, "\n")
}

// renderMessagesHeader renders the header line for the messages panel.
func (m Model) renderMessagesHeader() string {
	name := m.channelsView.DisplayNameByID(m.activeChannelID)
	if m.historyLoading {
		name = "⟳ " + name
	}
	msgsW := m.width - m.channelsWidth - 1
	if msgsW < 1 {
		msgsW = 1
	}
	var headerStyle lipgloss.Style
	if m.mode == ModeMessages {
		fg := m.activeHeaderFg
		if fg == "" {
			fg = "15"
		}
		bg := m.activeHeaderBg
		if bg == "" {
			bg = "237"
		}
		headerStyle = lipgloss.NewStyle().
			Bold(true).
			Width(msgsW).
			Foreground(lipgloss.Color(fg)).
			Background(lipgloss.Color(bg))
	} else {
		headerStyle = lipgloss.NewStyle().Bold(true).Width(msgsW).Foreground(lipgloss.Color("241"))
	}
	return headerStyle.Render(name)
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
		if item, ok := m.messagesView.SelectedItem(); ok && item.kind == feedItemKindMessage {
			msgID := item.msg.post.ID
			if len(msgID) > 8 {
				msgID = msgID[:8] + "..."
			}
			msg = fmt.Sprintf("selected: %s  (esc twice to unselect)", msgID)
		} else {
			msg = "Enter to send · Alt/Opt+Enter for newline · /send #channel · /quit to exit"
		}
	}
	color := lipgloss.Color("241")
	if m.statusIsError {
		color = lipgloss.Color("203")
	}

	if !m.showModeIndicator {
		return lipgloss.NewStyle().Width(m.width).Foreground(color).Render(msg)
	}

	modeLabels := map[Mode]string{
		ModeMessages: "[MESSAGES]",
		ModeChannels: "[CHANNELS]",
		ModeHelp:     "[HELP]",
	}
	badge, hasBadge := modeLabels[m.mode]
	if !hasBadge {
		return lipgloss.NewStyle().Width(m.width).Foreground(color).Render(msg)
	}
	badgeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.activeHeaderFg))
	styledBadge := badgeStyle.Render(badge)

	// Reserve space for the badge (plain-text width) so the status message
	// fills the remaining width without running into ANSI escape sequences.
	badgePlainW := len([]rune(badge))
	sep := " "
	msgW := m.width - badgePlainW - len([]rune(sep))
	if msgW < 0 {
		msgW = 0
	}
	styledMsg := lipgloss.NewStyle().Width(msgW).Foreground(color).Render(msg)
	return styledBadge + sep + styledMsg
}

func (m Model) renderInput() string {
	return m.input.View()
}

// helpDimensions computes the outer and inner dimensions of the help popup.
func (m Model) helpDimensions() (outerW, outerH, innerW, innerH int) {
	outerW = m.width - 4
	if outerW > 84 {
		outerW = 84
	}
	if outerW < 44 {
		outerW = 44
	}
	outerH = m.height - 4
	if outerH < 10 {
		outerH = 10
	}
	innerW = outerW - 2
	innerH = outerH - 2
	return
}

func (m Model) openHelp() (tea.Model, tea.Cmd) {
	m.prevMode = m.mode
	m.mode = ModeHelp
	_, _, innerW, innerH := m.helpDimensions()
	if !m.helpReady {
		m.helpViewport = viewport.New(innerW, innerH)
		m.helpReady = true
	} else {
		m.helpViewport.Width = innerW
		m.helpViewport.Height = innerH
	}
	m.helpViewport.SetContent(buildHelpContent(m.keys, m.registry, innerW))
	m.helpViewport.GotoTop()
	return m, nil
}

func (m Model) closeHelp() (tea.Model, tea.Cmd) {
	m.mode = m.prevMode
	return m, nil
}

func (m Model) handleKeyHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Cancel) {
		return m.closeHelp()
	}
	var cmd tea.Cmd
	m.helpViewport, cmd = m.helpViewport.Update(msg)
	return m, cmd
}

func (m Model) renderHelp() string {
	_, _, innerW, innerH := m.helpDimensions()

	content := "Loading..."
	if m.helpReady {
		content = m.helpViewport.View()
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(innerW).
		Height(innerH).
		Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
