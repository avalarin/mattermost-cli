package mattermost

// WSClient is a Mattermost WebSocket client stub.
type WSClient struct {
	url   string
	token string
}

// NewWSClient creates a new WebSocket client.
func NewWSClient(url, token string) *WSClient {
	return &WSClient{url: url, token: token}
}
