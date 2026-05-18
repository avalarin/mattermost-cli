package mattermost

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
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

// NewClient creates a new Mattermost REST client with no request timeout.
func NewClient(url, token string) *Client {
	return NewClientWithTimeout(url, token, 0)
}

// NewClientWithTimeout creates a new Mattermost REST client with the given request timeout.
// Use timeout=0 for no timeout (useful in tests with a local httptest server).
func NewClientWithTimeout(url, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL:    url,
		token:      token,
		httpClient: &http.Client{Timeout: timeout},
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

// getQ performs a GET request with a query string appended to the path.
func (c *Client) getQ(path, query string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/v4"+path+"?"+query, nil)
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

// CurrentUserID returns the cached current user ID (set after GetCurrentUser is called).
func (c *Client) CurrentUserID() string {
	return c.currentUserID
}

// GetUsersByIDs fetches profiles for the given user IDs.
// Returns a map from user ID to User.
func (c *Client) GetUsersByIDs(ids []string) (map[string]User, error) {
	var users []User
	if err := c.post("/users/ids", ids, &users); err != nil {
		return nil, err
	}
	result := make(map[string]User, len(users))
	for _, u := range users {
		result[u.ID] = u
	}
	return result, nil
}

// GetUserByUsername retrieves a user by their username.
func (c *Client) GetUserByUsername(username string) (*User, error) {
	var u User
	err := c.get("/users/username/"+url.PathEscape(username), &u)
	if errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetChannelPosts retrieves posts for a channel, returning them in chronological order (oldest first).
// page and perPage control pagination; page=0 returns the most recent perPage posts.
// The Mattermost v4 PostList response contains an "order" array (newest-first) and a "posts" map.
func (c *Client) GetChannelPosts(channelID string, page, perPage int) ([]Message, error) {
	type postList struct {
		Posts map[string]Message `json:"posts"`
		Order []string           `json:"order"`
	}
	var pl postList
	query := fmt.Sprintf("page=%d&per_page=%d", page, perPage)
	if err := c.getQ(fmt.Sprintf("/channels/%s/posts", channelID), query, &pl); err != nil {
		return nil, err
	}
	// Walk Order in reverse (last element = oldest) to build chronological slice.
	msgs := make([]Message, 0, len(pl.Order))
	for i := len(pl.Order) - 1; i >= 0; i-- {
		id := pl.Order[i]
		if msg, ok := pl.Posts[id]; ok {
			msgs = append(msgs, msg)
		}
	}
	return msgs, nil
}

// GetPostThread retrieves all posts in a thread, returning them in chronological order (oldest first).
// The rootID is the ID of the root message. The Mattermost v4 API returns a PostList
// with "order" (newest-first) and "posts" map, same shape as GetChannelPosts.
func (c *Client) GetPostThread(rootID string) ([]Message, error) {
	type postList struct {
		Posts map[string]Message `json:"posts"`
		Order []string           `json:"order"`
	}
	var pl postList
	if err := c.get(fmt.Sprintf("/posts/%s/thread", rootID), &pl); err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(pl.Order))
	for i := len(pl.Order) - 1; i >= 0; i-- {
		id := pl.Order[i]
		if msg, ok := pl.Posts[id]; ok {
			msgs = append(msgs, msg)
		}
	}
	return msgs, nil
}

// GetChannelUnreads returns the unread message and mention counts for a channel.
func (c *Client) GetChannelUnreads(channelID string) (*ChannelUnread, error) {
	var u ChannelUnread
	if err := c.get(fmt.Sprintf("/users/me/channels/%s/unread", channelID), &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// MarkChannelRead marks a channel as read for the current user.
func (c *Client) MarkChannelRead(channelID string) error {
	body := struct {
		ChannelID string `json:"channel_id"`
	}{ChannelID: channelID}
	return c.post(fmt.Sprintf("/channels/%s/members/me/view", channelID), body, nil)
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
