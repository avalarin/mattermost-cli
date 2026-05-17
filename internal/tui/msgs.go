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

// MsgOpenHelp signals that the help popup should be opened.
type MsgOpenHelp struct{}

// MsgChannelSelected is emitted when the user opens a channel from the channels panel.
type MsgChannelSelected struct {
	ChannelID string // empty string = All Activity
}
