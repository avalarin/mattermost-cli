package mattermost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// newTestWSServer starts a local WebSocket test server.
// The handler function receives the accepted connection.
func newTestWSServer(t *testing.T, handler func(conn *websocket.Conn)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Logf("ws accept error: %v", err)
			return
		}
		handler(conn)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wsURLFrom converts an httptest server URL (http://...) to a ws:// URL.
func wsURLFrom(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestWSConnect_SendsAuthChallenge(t *testing.T) {
	authReceived := make(chan map[string]interface{}, 1)

	srv := newTestWSServer(t, func(conn *websocket.Conn) {
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()

		var frame map[string]interface{}
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Logf("server read error: %v", err)
			return
		}
		authReceived <- frame
	})

	// The WSClient will try to connect to the WS URL directly.
	// We override url so toWSURL produces the test server URL.
	// toWSURL converts http:// → ws://, so we use the http URL here.
	c := &WSClient{
		url:    srv.URL,
		token:  "test-token",
		events: make(chan Event, 64),
		status: make(chan ConnStatus, 8),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// connect() runs synchronously until the server closes.
	go func() { _ = c.connect(ctx) }()

	select {
	case frame := <-authReceived:
		if frame["action"] != "authentication_challenge" {
			t.Errorf("expected action=authentication_challenge, got %v", frame["action"])
		}
		data, ok := frame["data"].(map[string]interface{})
		if !ok {
			t.Fatal("expected data field in auth frame")
		}
		if data["token"] != "test-token" {
			t.Errorf("expected token=test-token, got %v", data["token"])
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for auth frame")
	}
}

func TestWSReceivesPostedEvent(t *testing.T) {
	posted := Event{
		Type: EventTypePosted,
		Data: map[string]interface{}{
			"post":        `{"id":"p1","channel_id":"c1","user_id":"u1","message":"hello","create_at":1000}`,
			"sender_name": "@alice",
		},
	}

	srv := newTestWSServer(t, func(conn *websocket.Conn) {
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()

		// Read and discard the auth frame.
		var authFrame map[string]interface{}
		if err := wsjson.Read(ctx, conn, &authFrame); err != nil {
			return
		}

		// Send the posted event.
		if err := wsjson.Write(ctx, conn, posted); err != nil {
			t.Logf("server write error: %v", err)
		}
	})

	c := &WSClient{
		url:    srv.URL,
		token:  "test-token",
		events: make(chan Event, 64),
		status: make(chan ConnStatus, 8),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = c.connect(ctx) }()

	select {
	case evt := <-c.events:
		if evt.Type != EventTypePosted {
			t.Errorf("expected event type %q, got %q", EventTypePosted, evt.Type)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for posted event")
	}
}

func TestWSReconnectOnClose(t *testing.T) {
	// Count how many times the server accepts a connection.
	connectCount := make(chan struct{}, 10)

	srv := newTestWSServer(t, func(conn *websocket.Conn) {
		connectCount <- struct{}{}
		// Read auth frame then close immediately to force reconnect.
		ctx := context.Background()
		var authFrame map[string]interface{}
		_ = wsjson.Read(ctx, conn, &authFrame)
		_ = conn.CloseNow()
	})

	c := &WSClient{
		url:    srv.URL,
		token:  "tok",
		events: make(chan Event, 64),
		status: make(chan ConnStatus, 8),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c.Start(ctx)

	// Wait for at least two connections (initial + one reconnect).
	count := 0
	deadline := time.After(8 * time.Second)
	for count < 2 {
		select {
		case <-connectCount:
			count++
		case <-deadline:
			t.Fatalf("expected at least 2 connections, got %d", count)
		}
	}

	// Status channel should carry a "reconnecting..." message after the first drop.
	reconnectSeen := false
	timeout := time.After(8 * time.Second)
	for !reconnectSeen {
		select {
		case s := <-c.status:
			if strings.HasPrefix(string(s), "reconnecting") {
				reconnectSeen = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for reconnecting status")
		}
	}
}

func TestBackoffCapped(t *testing.T) {
	const maxAllowed = 60 * time.Second * 12 / 10 // 60s × 1.2 jitter cap

	for attempt := 0; attempt <= 20; attempt++ {
		d := backoffDuration(attempt)
		if d > maxAllowed {
			t.Errorf("attempt %d: backoff %v exceeds max allowed %v", attempt, d, maxAllowed)
		}
	}
}

func TestBackoffJitter(t *testing.T) {
	// Two calls at the same attempt should (almost always) differ due to jitter.
	// We run enough samples that equality is astronomically unlikely.
	const attempt = 5
	seen := make(map[time.Duration]struct{})
	for i := 0; i < 20; i++ {
		seen[backoffDuration(attempt)] = struct{}{}
	}
	if len(seen) < 2 {
		t.Error("expected jitter to produce different durations across calls")
	}
}

func TestToWSURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://example.com", "wss://example.com/api/v4/websocket"},
		{"http://localhost:8065", "ws://localhost:8065/api/v4/websocket"},
		{"https://example.com/", "wss://example.com/api/v4/websocket"},
		{"https://example.com//", "wss://example.com/api/v4/websocket"},
	}
	for _, tc := range cases {
		got := toWSURL(tc.in)
		if got != tc.want {
			t.Errorf("toWSURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestWSConnectSetsConnectedStatus verifies that the status channel receives
// ConnStatusConnected after a successful connection.
func TestWSConnectSetsConnectedStatus(t *testing.T) {
	srv := newTestWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		// Read and discard auth frame, then keep connection open briefly.
		var authFrame map[string]interface{}
		_ = wsjson.Read(ctx, conn, &authFrame)
		// Keep connection open briefly, then close.
		time.Sleep(100 * time.Millisecond)
		_ = conn.CloseNow()
	})

	c := &WSClient{
		url:    srv.URL,
		token:  "tok",
		events: make(chan Event, 64),
		status: make(chan ConnStatus, 8),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = c.connect(ctx) }()

	select {
	case s := <-c.status:
		if s != ConnStatusConnected {
			t.Errorf("expected ConnStatusConnected, got %q", s)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for connected status")
	}
}

// TestPostedEventJSONDecoding checks that Event unmarshals correctly from JSON.
func TestPostedEventJSONDecoding(t *testing.T) {
	raw := `{"event":"posted","data":{"post":"{\"id\":\"abc\",\"message\":\"hi\"}","sender_name":"@bob"}}`
	var evt Event
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if evt.Type != "posted" {
		t.Errorf("expected type=posted, got %q", evt.Type)
	}
	senderName, _ := evt.Data["sender_name"].(string)
	if senderName != "@bob" {
		t.Errorf("expected sender_name=@bob, got %q", senderName)
	}
}
