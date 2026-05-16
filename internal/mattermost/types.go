package mattermost

// Team represents a Mattermost team.
type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Channel represents a Mattermost channel.
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// User represents a Mattermost user.
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// Message represents a Mattermost post.
type Message struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Text      string `json:"message"`
	CreateAt  int64  `json:"create_at"`
	RootID    string `json:"root_id"`
}

// Event represents a Mattermost WebSocket event.
type Event struct {
	Type string                 `json:"event"`
	Data map[string]interface{} `json:"data"`
}

const EventTypePosted = "posted"
