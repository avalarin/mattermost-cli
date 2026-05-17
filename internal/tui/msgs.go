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
