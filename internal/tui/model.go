package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	// ModeThread is the thread popup mode: thread overlay is active.
	ModeThread
	// ModeSearch is the unified channel/user search + sort/filter popup mode.
	ModeSearch
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

	threadPopup         *ThreadPopup // non-nil when thread popup is open
	openThreadRootID    string       // root ID of the currently open thread
	replyRootID         string       // set by /reply; consumed by next /send
	threadPopupWidthPct int          // percent of terminal width for the thread popup (default 70)

	searchPopup       *SearchPopup       // non-nil when the search popup is open
	searchGen         int                // incremented on each search to discard stale MsgSearchResults
	channelSortFilter ChannelFilterState // currently applied sort/filter state

	unreadCounts map[string]int // channelID → unread message count

	ctx    context.Context
	cancel context.CancelFunc
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

	ctx, cancel := context.WithCancel(context.Background())
	return Model{
		input:         ta,
		keys:          DefaultKeyMap(),
		styles:        DefaultStyles(),
		statusMsg:     "",
		channelsWidth: 22,
		messagesView:  NewMessagesView(nil),
		activePage:    make(map[string]int),
		spinner:       sp,
		unreadCounts:  make(map[string]int),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// NewModelWithHeader creates a Model with pre-loaded header info, initial status,
// WebSocket channels, channel list, store, REST client, team ID, channels sidebar width,
// whether to display the mode indicator in the status bar, the full-date format
// used for messages not sent today (Go time layout, e.g. "02.01.2006"), the
// channel_messages setting ("root_only" | "all"), the initial channel sort order
// ("alphabetical" | "last_message"), and whether to show only unread channels.
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
	threadPopupWidthPct int,
	channelSort string,
	channelUnreadOnly bool,
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

	if threadPopupWidthPct > 0 {
		m.threadPopupWidthPct = threadPopupWidthPct
	} else {
		m.threadPopupWidthPct = 70
	}

	m.channelsRaw = channels
	m.channelsView = NewChannelsView(channels)

	sortOrder := ChannelSortAlphabetical
	if channelSort == "last_message" {
		sortOrder = ChannelSortLastMessage
	}
	m.channelSortFilter = ChannelFilterState{SortOrder: sortOrder, UnreadOnly: channelUnreadOnly}
	if sortOrder != ChannelSortAlphabetical || channelUnreadOnly {
		m.channelsView = m.channelsView.WithSortAndFilter(m.channelSortFilter, nil)
	}

	m.messagesView = NewMessagesView(st).SetFullDateFormat(fullDateFormat).SetAllActivity(true)
	m.registry = buildRegistry(client, teamID, m.cancel)

	return m
}

// buildRegistry constructs the command registry with all built-in commands.
func buildRegistry(client *mattermost.Client, teamID string, cancel context.CancelFunc) *Registry {
	r := NewRegistry()

	r.Register(&CommandDef{
		Name:        "quit",
		Description: "Exit the application",
		Execute:     func(_ map[string]string) tea.Cmd { cancel(); return tea.Quit },
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
		Name:        "reply",
		Description: "Set reply context for the open thread (next /send will post as a thread reply)",
		Execute: func(_ map[string]string) tea.Cmd {
			// Actual behavior handled in executeCommand before registry dispatch.
			return func() tea.Msg { return MsgCommandResult{Info: "Use /reply in a thread popup (press r)"} }
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

// makeSendReplyCmd is like makeSendCmd but sends with a thread rootID.
func makeSendReplyCmd(client *mattermost.Client, teamID, rootID string) func(map[string]string) tea.Cmd {
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
			if _, err := client.SendMessage(channelID, text, rootID); err != nil {
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

// loadThreadCmd returns a tea.Cmd that fetches a post thread from REST
// and batch-resolves author usernames.
func loadThreadCmd(client *mattermost.Client, postID string, selectedPostID string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return MsgThreadLoaded{RootID: postID, SelectedPostID: selectedPostID, Err: errors.New("not connected")}
		}
		msgs, err := client.GetPostThread(postID)
		if err != nil {
			return MsgThreadLoaded{RootID: postID, SelectedPostID: selectedPostID, Err: err}
		}
		seen := make(map[string]bool, len(msgs))
		var ids []string
		for _, msg := range msgs {
			if !seen[msg.UserID] {
				seen[msg.UserID] = true
				ids = append(ids, msg.UserID)
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
		// rootID: if the postID is a reply, the true root is its RootID.
		rootID := postID
		for _, msg := range msgs {
			if msg.ID == postID && msg.RootID != "" {
				rootID = msg.RootID
				break
			}
			if msg.RootID == "" {
				rootID = msg.ID
				break
			}
		}
		return MsgThreadLoaded{RootID: rootID, Messages: msgs, UserNames: userNames, SelectedPostID: selectedPostID}
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
		cmds = append(cmds, resolveDMNames(m.ctx, m.client, m.channelsRaw, m.store))
		if len(m.channelsRaw) > 0 && m.teamID != "" {
			cmds = append(cmds, loadUnreadsCmd(m.client, m.teamID, m.channelsRaw))
		}
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

// resolveDMNames fetches usernames for DM/group-DM channels and returns MsgDMNamesResolved.
// For Type=="D" it parses the channel name (userID1__userID2) to find the other user.
// For Type=="G" the display_name from the API may already be populated; if not, we skip
// (group DM name is an opaque hash, cannot be parsed into user IDs).
func resolveDMNames(ctx context.Context, client *mattermost.Client, channels []mattermost.Channel, st *store.Store) tea.Cmd {
	return func() tea.Msg {
		selfID := client.CurrentUserID()
		if selfID == "" {
			slog.Debug("resolveDMNames: selfID empty, skipping DM name resolution")
			return nil
		}
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
				slog.Debug("resolveDMNames: unexpected DM channel name format", "name", ch.Name)
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

		names := make(map[string]string) // channelID → username

		// Check cache first.
		if st != nil {
			cached, err := st.GetCachedUsernames(ids)
			if err != nil {
				slog.Debug("resolveDMNames: GetCachedUsernames failed", "err", err)
			}
			for otherID, username := range cached {
				if chID, ok := dmChannelIDs[otherID]; ok {
					names[chID] = username
				}
			}
		}

		// Build list of IDs not yet resolved from cache.
		var uncachedIDs []string
		for _, id := range ids {
			if chID, ok := dmChannelIDs[id]; ok {
				if _, resolved := names[chID]; !resolved {
					uncachedIDs = append(uncachedIDs, id)
				}
			}
		}
		if len(uncachedIDs) == 0 {
			return MsgDMNamesResolved{Names: names}
		}

		// Fetch uncached in batches of 100.
		const batchSize = 100
		freshUsers := make(map[string]string) // otherID → username
		for i := 0; i < len(uncachedIDs); i += batchSize {
			end := i + batchSize
			if end > len(uncachedIDs) {
				end = len(uncachedIDs)
			}
			batch := uncachedIDs[i:end]
			users, err := client.GetUsersByIDs(batch)
			if errors.Is(err, mattermost.ErrRateLimit) {
				slog.Debug("resolveDMNames: rate limited, retrying in 20s")
				select {
				case <-time.After(20 * time.Second):
				case <-ctx.Done():
					return MsgDMNamesResolved{Names: names}
				}
				users, err = client.GetUsersByIDs(batch)
			}
			if err != nil {
				slog.Debug("resolveDMNames: GetUsersByIDs batch failed", "err", err, "batch_start", i)
				continue
			}
			for otherID, u := range users {
				name := u.Username
				if name == "" {
					name = otherID
				}
				freshUsers[otherID] = name
				if chID, ok := dmChannelIDs[otherID]; ok {
					names[chID] = name
				}
			}
		}

		if st != nil && len(freshUsers) > 0 {
			if err := st.UpsertUsers(freshUsers); err != nil {
				slog.Debug("resolveDMNames: UpsertUsers failed", "err", err)
			}
		}

		return MsgDMNamesResolved{Names: names}
	}
}

// loadUnreadsCmd fetches unread counts for all channels in two batch requests:
// one for channel members (read positions) and one already available via the channel list.
// This avoids 1-per-channel requests that quickly exhaust the server's rate limit.
func loadUnreadsCmd(client *mattermost.Client, teamID string, channels []mattermost.Channel) tea.Cmd {
	return func() tea.Msg {
		members, err := client.GetMyChannelMembersForTeam(teamID)
		if err != nil {
			slog.Debug("loadUnreadsCmd: GetMyChannelMembersForTeam failed", "err", err)
			return MsgUnreadsLoaded{Counts: make(map[string]int)}
		}
		counts := make(map[string]int, len(channels))
		for _, ch := range channels {
			m, ok := members[ch.ID]
			if !ok {
				continue
			}
			unread := int(ch.TotalMsgCount - m.MsgCount)
			if unread < 0 {
				unread = 0
			}
			// Mentions are always surfaced even if the general unread math rounds to 0.
			if m.MentionCount > unread {
				unread = m.MentionCount
			}
			if unread > 0 {
				counts[ch.ID] = unread
			}
		}
		return MsgUnreadsLoaded{Counts: counts}
	}
}

// markReadCmd marks a channel as read and returns MsgChannelRead on completion.
func markReadCmd(client *mattermost.Client, channelID string) tea.Cmd {
	return func() tea.Msg {
		_ = client.MarkChannelRead(channelID)
		return MsgChannelRead{ChannelID: channelID}
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

	case MsgUnreadsLoaded:
		// Take the higher of the two counts: REST value covers history before startup,
		// WS increments cover messages that arrived while the fetch was in flight.
		for id, n := range msg.Counts {
			if n > m.unreadCounts[id] {
				m.unreadCounts[id] = n
			}
		}
		m.channelsView = m.channelsView.WithSortAndFilter(m.channelSortFilter, m.unreadCounts)
		return m, nil

	case MsgChannelRead:
		if m.unreadCounts != nil {
			m.unreadCounts[msg.ChannelID] = 0
		}
		if m.channelSortFilter.UnreadOnly {
			m.channelsView = m.channelsView.WithSortAndFilter(m.channelSortFilter, m.unreadCounts)
		}
		return m, nil

	case MsgSearchDebounce:
		if msg.Gen != m.searchGen || m.searchPopup == nil {
			return m, nil // stale debounce tick (user kept typing or closed popup)
		}
		// Start the spinner tick chain if it isn't already running (historyLoading keeps it alive otherwise).
		var spinnerCmd tea.Cmd
		if !m.historyLoading {
			spinnerCmd = m.spinner.Tick
		}
		return m, tea.Batch(searchCmd(m.client, m.teamID, msg.Query, msg.Gen), spinnerCmd)

	case MsgSearchResults:
		if msg.Gen != m.searchGen {
			return m, nil // stale result from a previous query
		}
		if m.searchPopup != nil {
			if msg.Err != nil {
				m.statusGen++
				m.statusMsg = "Search failed: " + msg.Err.Error()
				m.statusIsError = true
				gen := m.statusGen
				return m, clearStatusAfter(4*time.Second, gen)
			}
			p := m.searchPopup.SetSearchResults(msg.Channels, msg.Users)
			m.searchPopup = &p
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
		searchSpinning := m.searchPopup != nil && m.searchPopup.Searching()
		if !m.historyLoading && !searchSpinning {
			return m, nil
		}
		if m.historyLoading {
			m.messagesView = m.messagesView.SetFeedItems([]feedItem{
				{kind: feedItemKindSystem, system: m.spinner.View() + " Loading…"},
			})
		}
		return m, cmd

	case MsgOpenHelp:
		return m.openHelp()

	case MsgChannelSelected:
		prevChannelID := m.activeChannelID // capture before overwrite
		m.activeChannelID = msg.ChannelID
		m.channelsView = m.channelsView.SetOpenByID(msg.ChannelID)

		// Mark the previous channel as read when switching away from it.
		var markCmd tea.Cmd
		if m.client != nil && prevChannelID != "" && prevChannelID != msg.ChannelID {
			markCmd = markReadCmd(m.client, prevChannelID)
		}

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
			return m, markCmd
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
			markCmd,
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

	case MsgThreadLoaded:
		if !m.ready {
			return m, nil
		}
		if msg.Err != nil {
			m.statusMsg = "Failed to load thread: " + msg.Err.Error()
			m.statusIsError = true
			m.statusGen++
			return m, clearStatusAfter(3*time.Second, m.statusGen)
		}
		m.openThreadRootID = msg.RootID
		items := make([]feedItem, 0, len(msg.Messages))
		for _, p := range msg.Messages {
			name := msg.UserNames[p.UserID]
			if name == "" {
				name = p.UserID
			}
			chName := m.channels[p.ChannelID]
			if chName == "" {
				chName = p.ChannelID
			}
			isDM := m.channelTypes[p.ChannelID] == "D" || m.channelTypes[p.ChannelID] == "G"
			items = append(items, feedItem{
				kind:     feedItemKindMessage,
				createAt: p.CreateAt,
				msg: feedMessage{
					post:        p,
					senderName:  name,
					channelName: chName,
					isDM:        isDM,
				},
			})
		}
		// Pass the channel display label (e.g. "#general" or "@alice") only for real
		// channels; empty string when All Activity is active (no specific channel).
		channelDisplayName := ""
		if m.activeChannelID != "" {
			channelDisplayName = m.channelsView.DisplayNameByID(m.activeChannelID)
		}
		outerW, outerH := m.threadPopupDimensions()
		popup := NewThreadPopup(msg.RootID, channelDisplayName)
		if m.client != nil {
			popup.CurrentUserID = m.client.CurrentUserID()
		}
		popup = popup.SetSize(outerW, outerH)
		if msg.SelectedPostID != "" {
			popup = popup.SetFeedItems(items).SelectByID(msg.SelectedPostID)
		} else {
			popup = popup.SetFeedItems(items).SelectLast()
		}
		m.threadPopup = &popup
		m.mode = ModeThread
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

	if m.threadPopup != nil {
		outerW, outerH := m.threadPopupDimensions()
		tp := m.threadPopup.SetSize(outerW, outerH)
		m.threadPopup = &tp
	}

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
		if m.threadPopup != nil {
			outerW, outerH := m.threadPopupDimensions()
			tp := m.threadPopup.SetSize(outerW, outerH)
			m.threadPopup = &tp
		}
	}
	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Mode-specific Ctrl+C interceptions before the global exit handler.
	if m.mode == ModeHelp && key.Matches(msg, m.keys.CtrlC) {
		return m.closeHelp()
	}
	if m.mode == ModeSearch && key.Matches(msg, m.keys.CtrlC) {
		return m.closeSearchPopup(false)
	}
	// Ctrl+C is handled globally before mode dispatch — guaranteed exit path.
	if key.Matches(msg, m.keys.CtrlC) {
		return m.handleCtrlC()
	}

	// Ctrl+K opens (or closes) the unified search popup from any mode.
	if key.Matches(msg, m.keys.Search) {
		if m.mode == ModeSearch {
			return m.closeSearchPopup(false)
		}
		return m.openSearchPopup()
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
	case ModeThread:
		return m.handleKeyThread(msg)
	case ModeSearch:
		return m.handleKeySearch(msg)
	}
	return m, nil
}

// handleCtrlC implements the double-Ctrl+C exit mechanic.
func (m Model) handleCtrlC() (tea.Model, tea.Cmd) {
	if m.ctrlCPending {
		m.cancel()
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
	case key.Matches(msg, m.keys.Send): // enter → open thread popup
		if item, ok := m.messagesView.SelectedItem(); ok && item.kind == feedItemKindMessage {
			return m, loadThreadCmd(m.client, item.msg.post.ID, item.msg.post.ID)
		}
		return m, nil

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
		prevID := m.activeChannelID
		var channelID string
		m.channelsView, channelID = m.channelsView.OpenSelected()
		m.activeChannelID = channelID
		var markCmd tea.Cmd
		if m.client != nil && prevID != "" && prevID != channelID {
			markCmd = markReadCmd(m.client, prevID)
		}
		return m, tea.Batch(markCmd, func() tea.Msg { return MsgChannelSelected{ChannelID: channelID} })
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
			m.cancel()
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

	// Handle /reply specially: sets the reply context for the next /send.
	if text == "/reply" {
		if m.openThreadRootID != "" && m.threadPopup != nil {
			m.replyRootID = m.openThreadRootID
			m.statusMsg = "Reply context set — use /send #channel text to reply"
			m.statusIsError = false
			m.statusGen++
			return m, clearStatusAfter(4*time.Second, m.statusGen)
		}
		m.statusMsg = "No thread open — open a thread first (Enter on a message)"
		m.statusIsError = true
		m.statusGen++
		return m, clearStatusAfter(3*time.Second, m.statusGen)
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

	// Inject replyRootID into /send if reply context is set.
	if result.Def.Name == "send" && m.replyRootID != "" {
		rootID := m.replyRootID
		m.replyRootID = "" // consume the context
		cmd := makeSendReplyCmd(m.client, m.teamID, rootID)(result.Args)
		return m, cmd
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

	// Increment unread count when a message arrives in a non-active channel.
	// Only when a specific channel is open (not All Activity), and only for other channels.
	if m.activeChannelID != "" && post.ChannelID != m.activeChannelID {
		if m.unreadCounts == nil {
			m.unreadCounts = make(map[string]int)
		}
		m.unreadCounts[post.ChannelID]++
		if m.channelSortFilter.UnreadOnly {
			m.channelsView = m.channelsView.WithSortAndFilter(m.channelSortFilter, m.unreadCounts)
		}
	}

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

	// If a thread popup is open and this post belongs to that thread, add it there too.
	if m.threadPopup != nil && m.openThreadRootID != "" &&
		(post.RootID == m.openThreadRootID || post.ID == m.openThreadRootID) {
		tp := m.threadPopup.AddFeedItem(feedItem{
			kind:     feedItemKindMessage,
			createAt: post.CreateAt,
			msg: feedMessage{
				post:        post,
				senderName:  senderName,
				channelName: channelName,
				isDM:        isDM,
			},
		})
		m.threadPopup = &tp
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

	var body string
	if m.searchPopup != nil {
		body = lipgloss.Place(m.width, m.feedH, lipgloss.Center, lipgloss.Center, m.searchPopup.View(m.spinner.View()))
	} else if m.threadPopup != nil {
		body = lipgloss.Place(m.width, m.feedH, lipgloss.Center, lipgloss.Center, m.threadPopup.View())
	} else {
		body = m.renderBody()
	}

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
	channelsPanel := m.channelsView.SetActive(m.mode == ModeChannels).SetActiveFg(m.activeHeaderFg).SetActiveBg(m.activeHeaderBg).SetUnreadCounts(m.unreadCounts).View()
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
	// Compute badge first so we know the actual width available for the message.
	modeLabels := map[Mode]string{
		ModeMessages:      "[MESSAGES]",
		ModeChannels:      "[CHANNELS]",
		ModeHelp:          "[HELP]",
		ModeThread:        "[THREAD]",
		ModeSearch: "[SEARCH]",
	}
	badge, hasBadge := modeLabels[m.mode]
	if !m.showModeIndicator {
		hasBadge = false
	}

	const badgeSep = " "
	msgW := m.width
	if hasBadge {
		msgW = m.width - len([]rune(badge)) - len([]rune(badgeSep))
	}
	if msgW < 0 {
		msgW = 0
	}

	msg := m.statusMsg
	if msg == "" {
		msg = m.buildStatusMsg(msgW)
	}

	// Truncate to one line: if the message is still wider than msgW, cut it.
	if msgW > 0 && len([]rune(msg)) > msgW {
		msg = string([]rune(msg)[:msgW-1]) + "…"
	}

	color := lipgloss.Color("241")
	if m.statusIsError {
		color = lipgloss.Color("203")
	}

	if !hasBadge {
		return lipgloss.NewStyle().Width(m.width).Foreground(color).Render(msg)
	}
	badgeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.activeHeaderFg))
	styledBadge := badgeStyle.Render(badge)
	styledMsg := lipgloss.NewStyle().Width(msgW).Foreground(color).Render(msg)
	return styledBadge + badgeSep + styledMsg
}

// buildStatusMsg returns the context string for the status bar when there is no
// explicit status message. It receives the available display width so it can
// truncate the message preview to exactly fit one line.
func (m Model) buildStatusMsg(availW int) string {
	fitPreview := func(sender, text, suffix string) string {
		// Collapse newlines so the preview stays on one line.
		text = strings.ReplaceAll(text, "\n", " ")
		// "[sender] " + preview + suffix must fit in availW runes.
		prefix := fmt.Sprintf("[%s] ", sender)
		fixed := len([]rune(prefix)) + len([]rune(suffix))
		previewW := availW - fixed
		runes := []rune(text)
		if previewW <= 0 {
			return suffix
		}
		if len(runes) > previewW {
			text = string(runes[:previewW-1]) + "…"
		}
		return prefix + text + suffix
	}

	if m.threadPopup != nil {
		if item, ok := m.threadPopup.SelectedItem(); ok && item.kind == feedItemKindMessage {
			return fitPreview(item.msg.senderName, item.msg.post.Text, "  · r reply · e edit · d delete · Esc close")
		}
		return "r reply · e edit · d delete · Esc close"
	}
	if item, ok := m.messagesView.SelectedItem(); ok && item.kind == feedItemKindMessage {
		return fitPreview(item.msg.senderName, item.msg.post.Text, "  · Enter to open thread · Esc to unselect")
	}
	return "Enter to send · Alt/Opt+Enter for newline · /send #channel · /quit to exit"
}

func (m Model) renderInput() string {
	return m.input.View()
}

// threadPopupDimensions returns the outer (W, H) for the thread popup.
func (m Model) threadPopupDimensions() (outerW, outerH int) {
	pct := m.threadPopupWidthPct
	if pct <= 0 {
		pct = 70
	}
	outerW = m.width * pct / 100
	if outerW < 20 {
		outerW = 20
	}
	if outerW > m.width-2 {
		outerW = m.width - 2
	}
	outerH = m.feedH
	if outerH < 6 {
		outerH = 6
	}
	return
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

func (m Model) handleKeyThread(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.threadPopup == nil {
		m.mode = ModeMessages
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Cancel): // Esc → close popup, back to messages
		m.threadPopup = nil
		m.openThreadRootID = ""
		m.replyRootID = ""
		m.mode = ModeMessages
		return m, nil

	case key.Matches(msg, m.keys.Up):
		tp := m.threadPopup.MoveCursorUp()
		m.threadPopup = &tp
		return m, nil

	case key.Matches(msg, m.keys.Down):
		tp := m.threadPopup.MoveCursorDown()
		m.threadPopup = &tp
		return m, nil

	case key.Matches(msg, m.keys.PageUp):
		tp := m.threadPopup.MoveCursorUpN(m.threadPopup.PageSize())
		m.threadPopup = &tp
		return m, nil

	case key.Matches(msg, m.keys.PageDown):
		tp := m.threadPopup.MoveCursorDownN(m.threadPopup.PageSize())
		m.threadPopup = &tp
		return m, nil
	}

	// Action keys on the selected message.
	item, hasItem := m.threadPopup.SelectedItem()
	if hasItem && item.kind == feedItemKindMessage {
		switch {
		case msg.Type == tea.KeyRunes && string(msg.Runes) == "r":
			m.input.SetValue("/reply")
			m.mode = ModeInput
			m.input.Focus() //nolint:errcheck
			return m, nil

		case msg.Type == tea.KeyRunes && string(msg.Runes) == "e":
			if item.msg.post.UserID == m.threadPopup.CurrentUserID {
				m.input.SetValue("/edit " + item.msg.post.Text)
				m.mode = ModeInput
				m.input.Focus() //nolint:errcheck
			}
			return m, nil

		case msg.Type == tea.KeyRunes && string(msg.Runes) == "d":
			if item.msg.post.UserID == m.threadPopup.CurrentUserID {
				m.statusMsg = "Delete: not implemented yet"
				m.statusIsError = false
				m.statusGen++
				return m, clearStatusAfter(3*time.Second, m.statusGen)
			}
			return m, nil
		}
	}

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

func (m Model) openSearchPopup() (tea.Model, tea.Cmd) {
	outerW := m.width - 4
	if outerW > 62 {
		outerW = 62
	}
	if outerW < 20 {
		outerW = 20
	}
	outerH := m.height - 6
	if outerH > 22 {
		outerH = 22
	}
	channels := m.channelsView.ChannelList()
	popup := NewSearchPopup(m.channelSortFilter, channels, m.unreadCounts)
	popup = popup.SetSize(outerW, outerH)
	m.searchPopup = &popup
	m.prevMode = m.mode
	m.mode = ModeSearch
	return m, nil
}

func (m Model) closeSearchPopup(apply bool) (tea.Model, tea.Cmd) {
	if m.searchPopup != nil {
		if apply {
			m.channelSortFilter = m.searchPopup.Filter()
		} else {
			m.channelSortFilter = m.searchPopup.Original()
		}
		m.channelsView = m.channelsView.WithSortAndFilter(m.channelSortFilter, m.unreadCounts)
	}
	m.searchPopup = nil
	m.mode = m.prevMode
	return m, nil
}

func (m Model) handleKeySearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchPopup == nil {
		return m, nil
	}
	p := *m.searchPopup

	switch {
	case key.Matches(msg, m.keys.Cancel): // Esc — close, restore original filter
		return m.closeSearchPopup(false)

	case key.Matches(msg, m.keys.Up):
		p = p.MoveUp()
		m.searchPopup = &p
		return m, nil

	case key.Matches(msg, m.keys.Down):
		p = p.MoveDown()
		m.searchPopup = &p
		return m, nil

	case key.Matches(msg, m.keys.Left):
		p = p.MoveLeft()
		m.searchPopup = &p
		return m, nil

	case key.Matches(msg, m.keys.Right):
		p = p.MoveRight()
		m.searchPopup = &p
		return m, nil

	case msg.String() == "tab":
		p = p.ToggleFocus()
		m.searchPopup = &p
		return m, nil

	case msg.String() == " ":
		p = p.ToggleFilter()
		m.searchPopup = &p
		return m, nil

	case key.Matches(msg, m.keys.Send): // Enter — open selected item
		item, ok := p.SelectedItem()
		if !ok {
			return m, nil
		}
		switch item.kind {
		case searchResultAllActivity:
			m2, cmd2 := m.closeSearchPopup(true)
			return m2, tea.Batch(cmd2, func() tea.Msg { return MsgChannelSelected{ChannelID: ""} })
		case searchResultChannel:
			chID := item.channel.ID
			m2, cmd2 := m.closeSearchPopup(true)
			return m2, tea.Batch(cmd2, func() tea.Msg { return MsgChannelSelected{ChannelID: chID} })
		case searchResultUser:
			userID := item.user.ID
			m2, cmd2 := m.closeSearchPopup(true)
			return m2, tea.Batch(cmd2, findOrCreateDMCmd(m.client, m.teamID, userID))
		}
		return m, nil

	case msg.Type == tea.KeyBackspace:
		p = p.Backspace()
		m.searchPopup = &p
		return m, nil

	default:
		if len(msg.Runes) > 0 {
			for _, r := range msg.Runes {
				p = p.TypeChar(r)
			}
			if p.IsSearchMode() {
				p = p.SetSearching()
				m.searchGen++
				gen := m.searchGen
				query := p.Query()
				m.searchPopup = &p
				return m, tea.Tick(searchDebounceDelay, func(_ time.Time) tea.Msg {
					return MsgSearchDebounce{Gen: gen, Query: query}
				})
			}
			m.searchPopup = &p
		}
	}
	return m, nil
}

// searchDebounceDelay is the wait after the last keystroke before firing the REST search.
const searchDebounceDelay = 300 * time.Millisecond

// searchCmd fires a REST search for channels and users and returns MsgSearchResults.
func searchCmd(client *mattermost.Client, teamID, query string, gen int) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return MsgSearchResults{Gen: gen, Err: errors.New("not connected")}
		}
		channels, chErr := client.SearchChannels(teamID, query)
		users, usrErr := client.SearchUsers(query)
		if chErr != nil {
			return MsgSearchResults{Gen: gen, Err: chErr}
		}
		if usrErr != nil {
			return MsgSearchResults{Gen: gen, Err: usrErr}
		}
		return MsgSearchResults{Gen: gen, Channels: channels, Users: users}
	}
}

// findOrCreateDMCmd creates or finds a DM channel with the given user and returns MsgChannelSelected.
func findOrCreateDMCmd(client *mattermost.Client, teamID, userID string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return MsgCommandResult{Err: errors.New("not connected")}
		}
		ch, err := client.FindOrCreateDM(teamID, userID)
		if err != nil {
			return MsgCommandResult{Err: fmt.Errorf("open DM: %w", err)}
		}
		return MsgChannelSelected{ChannelID: ch.ID}
	}
}
