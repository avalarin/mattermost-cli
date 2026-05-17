package mattermost

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// WSClient is a Mattermost WebSocket client that reconnects with exponential backoff.
type WSClient struct {
	url    string
	token  string
	events chan Event
	status chan ConnStatus
}

// NewWSClient creates a new WebSocket client.
func NewWSClient(url, token string) *WSClient {
	return &WSClient{
		url:    url,
		token:  token,
		events: make(chan Event, 64),
		status: make(chan ConnStatus, 8),
	}
}

// Events returns the read-only channel of incoming WebSocket events.
func (c *WSClient) Events() <-chan Event { return c.events }

// Status returns the read-only channel of connection status updates.
func (c *WSClient) Status() <-chan ConnStatus { return c.status }

// Start begins the WS connection loop in a background goroutine.
// The loop reconnects with exponential backoff until ctx is cancelled.
func (c *WSClient) Start(ctx context.Context) {
	go c.run(ctx)
}

func (c *WSClient) run(ctx context.Context) {
	defer func() {
		close(c.events)
		close(c.status)
	}()

	attempt := 0
	for {
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			// Clean disconnect (no error) — reset backoff so the next
			// reconnect starts fast rather than from the last long delay.
			attempt = 0
		} else {
			slog.Debug("ws connect error", "err", err)
		}
		d := backoffDuration(attempt)
		attempt++
		// Send countdown ticks every second during the backoff delay.
		deadline := time.Now().Add(d)
		for {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			secs := int(remaining.Seconds()) + 1
			select {
			case c.status <- ConnStatus(fmt.Sprintf("reconnecting... %ds", secs)):
			default:
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (c *WSClient) connect(ctx context.Context) error {
	wsURL := toWSURL(c.url)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Send authentication challenge.
	authFrame := map[string]interface{}{
		"seq":    1,
		"action": "authentication_challenge",
		"data":   map[string]interface{}{"token": c.token},
	}
	if err := wsjson.Write(ctx, conn, authFrame); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// Drain any stale reconnecting-countdown statuses so ConnStatusConnected
	// is not silently dropped when the buffer is full.
	for {
		select {
		case <-c.status:
		default:
			goto drained
		}
	}
drained:
	c.status <- ConnStatusConnected

	// Read loop: forward events until the connection drops or ctx is cancelled.
	for {
		var event Event
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}
		select {
		case c.events <- event:
		case <-ctx.Done():
			return nil
		}
	}
}

// toWSURL converts an http/https URL to ws/wss and appends the Mattermost WS path.
func toWSURL(serverURL string) string {
	url := serverURL
	switch {
	case len(url) >= 8 && url[:8] == "https://":
		url = "wss://" + url[8:]
	case len(url) >= 7 && url[:7] == "http://":
		url = "ws://" + url[7:]
	}
	// Remove trailing slash.
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	return url + "/api/v4/websocket"
}

// backoffDuration returns the reconnect delay for the given attempt number.
// Formula: min(base * 2^attempt, 60s) × jitter[0.8, 1.2].
func backoffDuration(attempt int) time.Duration {
	const base = time.Second
	const cap = 60 * time.Second
	shift := attempt
	if shift > 10 {
		shift = 10
	}
	d := base * (1 << uint(shift))
	if d > cap {
		d = cap
	}
	jitter := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(d) * jitter)
}
