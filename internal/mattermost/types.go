package mattermost

// Team represents a Mattermost team.
type Team struct {
	ID   string
	Name string
}

// Channel represents a Mattermost channel.
type Channel struct {
	ID   string
	Name string
}

// User represents a Mattermost user.
type User struct {
	ID       string
	Username string
}

// Message represents a Mattermost message.
type Message struct {
	ID        string
	ChannelID string
	UserID    string
	Text      string
	CreateAt  int64
	RootID    string
}

// Event represents a Mattermost WebSocket event.
type Event struct {
	Type string
	Data map[string]interface{}
}
