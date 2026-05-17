package tui

import "github.com/avalarin/mattermost-cli/internal/mattermost"

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
type MsgClearStatus struct{}

// MsgNewMessage carries an individual message (used for startup history delivery).
type MsgNewMessage struct {
	Post        mattermost.Message
	SenderName  string
	ChannelName string
}

// MsgHistoryLoaded signals completion of the startup history load.
// Lines contains the pre-rendered display strings in chronological order.
type MsgHistoryLoaded struct {
	Lines []string
	Err   error
}
