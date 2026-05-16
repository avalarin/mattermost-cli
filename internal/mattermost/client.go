package mattermost

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var (
	// ErrChannelNotFound is returned by GetChannelByName when the channel does not exist.
	ErrChannelNotFound = errors.New("channel not found")
	// ErrAuthFailed is returned when the API token is invalid or expired.
	ErrAuthFailed = errors.New("authentication failed: invalid token")

	errNotFound = errors.New("not found")
)

// Client is a Mattermost REST API v4 client.
type Client struct {
	baseURL       string
	token         string
	httpClient    *http.Client
	currentUserID string // cached after GetCurrentUser
}

// NewClient creates a new Mattermost REST client.
func NewClient(url, token string) *Client {
	return &Client{
		baseURL:    url,
		token:      token,
		httpClient: &http.Client{},
	}
}

func (c *Client) get(path string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/v4"+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return c.do(req, out)
}

func (c *Client) post(path string, body interface{}, out interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/v4"+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrAuthFailed
	case http.StatusNotFound:
		return errNotFound
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// GetCurrentUser retrieves the authenticated user and caches their ID.
func (c *Client) GetCurrentUser() (*User, error) {
	var u User
	if err := c.get("/users/me", &u); err != nil {
		return nil, err
	}
	c.currentUserID = u.ID
	return &u, nil
}

// GetTeamByName retrieves a team by its slug name.
func (c *Client) GetTeamByName(name string) (*Team, error) {
	var t Team
	if err := c.get("/teams/name/"+name, &t); err != nil {
		if errors.Is(err, errNotFound) {
			return nil, fmt.Errorf("team not found: %s", name)
		}
		return nil, err
	}
	return &t, nil
}

// GetChannelsForTeam retrieves all channels the current user is a member of in a team.
func (c *Client) GetChannelsForTeam(teamID string) ([]Channel, error) {
	var channels []Channel
	if err := c.get(fmt.Sprintf("/users/me/teams/%s/channels", teamID), &channels); err != nil {
		return nil, err
	}
	return channels, nil
}

// GetChannelByName retrieves a channel by its slug name within a team.
// Returns ErrChannelNotFound if the channel does not exist.
func (c *Client) GetChannelByName(teamID, name string) (*Channel, error) {
	var ch Channel
	err := c.get(fmt.Sprintf("/teams/%s/channels/name/%s", teamID, name), &ch)
	if errors.Is(err, errNotFound) {
		return nil, ErrChannelNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// FindOrCreateDM finds or creates a direct message channel with the target user.
// Uses the cached current user ID from GetCurrentUser; fetches it if not cached.
func (c *Client) FindOrCreateDM(teamID, targetUserID string) (*Channel, error) {
	currentUserID := c.currentUserID
	if currentUserID == "" {
		u, err := c.GetCurrentUser()
		if err != nil {
			return nil, fmt.Errorf("get current user for DM: %w", err)
		}
		currentUserID = u.ID
	}
	var ch Channel
	if err := c.post("/channels/direct", []string{currentUserID, targetUserID}, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

// SendMessage sends a message to a channel. Set rootID to post as a thread reply.
func (c *Client) SendMessage(channelID, text, rootID string) (*Message, error) {
	body := struct {
		ChannelID string `json:"channel_id"`
		Message   string `json:"message"`
		RootID    string `json:"root_id,omitempty"`
	}{
		ChannelID: channelID,
		Message:   text,
		RootID:    rootID,
	}
	var msg Message
	if err := c.post("/posts", body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
