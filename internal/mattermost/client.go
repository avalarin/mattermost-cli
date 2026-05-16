package mattermost

import "fmt"

// Client is a Mattermost REST API client.
type Client struct {
	url   string
	token string
}

// NewClient creates a new Mattermost REST client.
func NewClient(url, token string) *Client {
	return &Client{url: url, token: token}
}

// GetTeamByName retrieves a team by its slug name.
func (c *Client) GetTeamByName(name string) (*Team, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetCurrentUser retrieves the authenticated user.
func (c *Client) GetCurrentUser() (*User, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetChannelsForTeam retrieves all channels for a team.
func (c *Client) GetChannelsForTeam(teamID string) ([]Channel, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetChannelByName retrieves a channel by its slug name within a team.
func (c *Client) GetChannelByName(teamID, name string) (*Channel, error) {
	return nil, fmt.Errorf("not implemented")
}

// FindOrCreateDM finds or creates a DM channel with the target user.
func (c *Client) FindOrCreateDM(teamID, targetUserID string) (*Channel, error) {
	return nil, fmt.Errorf("not implemented")
}

// SendMessage sends a message to a channel.
func (c *Client) SendMessage(channelID, text, rootID string) (*Message, error) {
	return nil, fmt.Errorf("not implemented")
}
