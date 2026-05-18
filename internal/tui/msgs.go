package tui

import (
	"github.com/avalarin/mattermost-cli/internal/mattermost"
	"github.com/avalarin/mattermost-cli/internal/store"
)

// MsgConnStatus carries a WebSocket connection status update.
type MsgConnStatus struct {
	Status mattermost.ConnStatus
}

// MsgCommandResult carries the result of an executed command.
type MsgCommandResult struct {
	Err  error
	Info string
}

// MsgClearStatus signals that the status bar should be cleared.
// Gen must match the model's current statusGen to prevent a stale timer
// from wiping a more recent command result.
type MsgClearStatus struct{ Gen int }

// MsgNewMessage carries an individual message (used for startup history delivery).
type MsgNewMessage struct {
	Post        mattermost.Message
	SenderName  string
	ChannelName string
}

// MsgHistoryLoaded signals completion of the startup history load.
// Messages contains the raw store messages in chronological order.
type MsgHistoryLoaded struct {
	Messages []store.Message
	Err      error
}

// MsgSystemMessage is a system-generated text block appended to the feed (e.g., /help output).
type MsgSystemMessage struct {
	Text string
}

// MsgEscTimeout signals that the double-esc window has expired.
type MsgEscTimeout struct{ Gen int }

// MsgCtrlCTimeout signals that the double-ctrl+c window has expired.
type MsgCtrlCTimeout struct{ Gen int }

// MsgPrefixTimeout signals that the prefix-key window has expired.
type MsgPrefixTimeout struct{ Gen int }

// MsgOpenHelp signals that the help popup should be opened.
type MsgOpenHelp struct{}

// MsgChannelSelected is emitted when the user opens a channel from the channels panel.
type MsgChannelSelected struct {
	ChannelID string // empty string = All Activity
}

// MsgDMNamesResolved carries resolved display names for DM channels.
type MsgDMNamesResolved struct {
	// Names maps channel ID to the resolved username (e.g. "alice", without the @ sigil).
	// The @ prefix is added by the rendering layer (channelLabel).
	Names map[string]string
}

// MsgChannelHistory carries the result of a channel history REST load.
type MsgChannelHistory struct {
	ChannelID string
	Messages  []mattermost.Message
	Prepend   bool
	Err       error
	UserNames map[string]string // userID → resolved username; nil if resolution failed
}

// MsgChannelHistoryLoading signals that history is being fetched for a channel.
type MsgChannelHistoryLoading struct {
	ChannelID string
}

// MsgRequestReload is emitted by the /reload command to trigger a history reload.
type MsgRequestReload struct{}

// MsgResetCaches signals that all in-memory caches should be cleared.
type MsgResetCaches struct{}

// MsgResetDB signals that the database should be wiped in addition to in-memory caches.
type MsgResetDB struct{}

// MsgUnreadsLoaded carries the initial unread counts for all channels.
// Individual channel fetch failures are silently skipped; missing keys mean zero unreads.
type MsgUnreadsLoaded struct {
	Counts map[string]int
}

// MsgChannelRead signals that a channel has been successfully marked as read.
type MsgChannelRead struct {
	ChannelID string
}

// MsgThreadLoaded carries the result of a thread REST load.
type MsgThreadLoaded struct {
	RootID         string
	Messages       []mattermost.Message
	UserNames      map[string]string // userID → resolved username
	SelectedPostID string            // post ID to highlight on open (the one Enter was pressed on)
	Err            error
}
