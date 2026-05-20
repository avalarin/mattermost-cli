package mattermost_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
)

func newTestServer(t *testing.T, handler http.Handler) (*httptest.Server, *mattermost.Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, mattermost.NewClient(srv.URL, "test-token")
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func TestGetCurrentUser_OK(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/users/me" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]string{"id": "user123", "username": "alice"})
	}))

	user, err := client.GetCurrentUser()
	if err != nil {
		t.Fatalf("GetCurrentUser() error: %v", err)
	}
	if user.ID != "user123" {
		t.Errorf("user.ID = %q, want %q", user.ID, "user123")
	}
	if user.Username != "alice" {
		t.Errorf("user.Username = %q, want %q", user.Username, "alice")
	}
}

func TestGetTeamByName_NotFound(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.GetTeamByName("nonexistent")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestGetChannelByName_NotFound(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.GetChannelByName("team123", "nonexistent")
	if !errors.Is(err, mattermost.ErrChannelNotFound) {
		t.Errorf("expected ErrChannelNotFound, got: %v", err)
	}
}

func TestSendMessage_AuthHeader(t *testing.T) {
	var capturedAuth string
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		writeJSON(w, map[string]interface{}{"id": "post123", "channel_id": "chan123", "message": "hello"})
	}))

	if _, err := client.SendMessage("chan123", "hello", ""); err != nil {
		t.Fatalf("SendMessage() error: %v", err)
	}
	if want := "Bearer test-token"; capturedAuth != want {
		t.Errorf("Authorization = %q, want %q", capturedAuth, want)
	}
}

func TestSendMessage_OK(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]interface{}{"id": "post123", "channel_id": "chan123", "message": "hello"})
	}))

	msg, err := client.SendMessage("chan123", "hello", "")
	if err != nil {
		t.Fatalf("SendMessage() error: %v", err)
	}
	if msg.ID != "post123" {
		t.Errorf("msg.ID = %q, want %q", msg.ID, "post123")
	}
}

func TestGetUserByUsername_OK(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/users/username/alice" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]string{"id": "user456", "username": "alice"})
	}))

	user, err := client.GetUserByUsername("alice")
	if err != nil {
		t.Fatalf("GetUserByUsername() error: %v", err)
	}
	if user.ID != "user456" {
		t.Errorf("user.ID = %q, want %q", user.ID, "user456")
	}
	if user.Username != "alice" {
		t.Errorf("user.Username = %q, want %q", user.Username, "alice")
	}
}

func TestGetUserByUsername_NotFound(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.GetUserByUsername("nonexistent")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "user not found") {
		t.Errorf("expected 'user not found' in error, got: %v", err)
	}
}

func TestGetChannelPosts_OK(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/channels/chan1/posts" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]interface{}{
			"order": []string{"id2", "id1"},
			"posts": map[string]interface{}{
				"id1": map[string]interface{}{
					"id": "id1", "channel_id": "chan1", "user_id": "u1",
					"message": "first", "create_at": 1000, "root_id": "", "reply_count": 2,
				},
				"id2": map[string]interface{}{
					"id": "id2", "channel_id": "chan1", "user_id": "u2",
					"message": "second", "create_at": 2000, "root_id": "", "reply_count": 0,
				},
			},
		})
	}))

	msgs, err := client.GetChannelPosts("chan1", 0, 100)
	if err != nil {
		t.Fatalf("GetChannelPosts() error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	// Order should be oldest-first (id1 before id2).
	if msgs[0].ID != "id1" {
		t.Errorf("msgs[0].ID = %q, want %q", msgs[0].ID, "id1")
	}
	if msgs[1].ID != "id2" {
		t.Errorf("msgs[1].ID = %q, want %q", msgs[1].ID, "id2")
	}
	// ReplyCount must be mapped.
	if msgs[0].ReplyCount != 2 {
		t.Errorf("msgs[0].ReplyCount = %d, want 2", msgs[0].ReplyCount)
	}
}

func TestGetChannelPosts_Pagination(t *testing.T) {
	var capturedQuery string
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		writeJSON(w, map[string]interface{}{"order": []string{}, "posts": map[string]interface{}{}})
	}))

	_, err := client.GetChannelPosts("chan1", 3, 50)
	if err != nil {
		t.Fatalf("GetChannelPosts() error: %v", err)
	}
	if capturedQuery != "page=3&per_page=50" {
		t.Errorf("query = %q, want %q", capturedQuery, "page=3&per_page=50")
	}
}

func TestGetPostThread_OK(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/posts/root1/thread" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]interface{}{
			"order": []string{"reply1", "root1"},
			"posts": map[string]interface{}{
				"root1":  map[string]interface{}{"id": "root1", "message": "root", "user_id": "u1", "create_at": 1000, "root_id": "", "reply_count": 1},
				"reply1": map[string]interface{}{"id": "reply1", "message": "reply", "user_id": "u2", "create_at": 2000, "root_id": "root1", "reply_count": 0},
			},
		})
	}))

	msgs, err := client.GetPostThread("root1")
	if err != nil {
		t.Fatalf("GetPostThread() error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	// Chronological order: root first, then reply.
	if msgs[0].ID != "root1" {
		t.Errorf("msgs[0].ID = %q, want %q", msgs[0].ID, "root1")
	}
	if msgs[1].ID != "reply1" {
		t.Errorf("msgs[1].ID = %q, want %q", msgs[1].ID, "reply1")
	}
	if msgs[0].ReplyCount != 1 {
		t.Errorf("msgs[0].ReplyCount = %d, want 1", msgs[0].ReplyCount)
	}
	if msgs[1].RootID != "root1" {
		t.Errorf("msgs[1].RootID = %q, want %q", msgs[1].RootID, "root1")
	}
}

func TestMessageIsReply(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]interface{}{
			"id":         "post456",
			"channel_id": "chan123",
			"message":    "reply text",
			"root_id":    "parent123",
		})
	}))

	msg, err := client.SendMessage("chan123", "reply text", "parent123")
	if err != nil {
		t.Fatalf("SendMessage() error: %v", err)
	}
	if msg.RootID == "" {
		t.Error("expected RootID to be set for a thread reply")
	}
	if msg.RootID != "parent123" {
		t.Errorf("msg.RootID = %q, want %q", msg.RootID, "parent123")
	}
}

func TestGetChannelUnreads_OK(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/users/me/channels/chan1/unread" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]interface{}{"channel_id": "chan1", "msg_count": 3, "mention_count": 1})
	}))

	u, err := client.GetChannelUnreads("chan1")
	if err != nil {
		t.Fatalf("GetChannelUnreads() error: %v", err)
	}
	if u.ChannelID != "chan1" {
		t.Errorf("ChannelID = %q, want %q", u.ChannelID, "chan1")
	}
	if u.MsgCount != 3 {
		t.Errorf("MsgCount = %d, want 3", u.MsgCount)
	}
	if u.MentionCount != 1 {
		t.Errorf("MentionCount = %d, want 1", u.MentionCount)
	}
}

func TestGetChannelUnreads_NotFound(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.GetChannelUnreads("missing")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestMarkChannelRead_CallsAPI(t *testing.T) {
	var capturedPath string
	var capturedMethod string
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.MarkChannelRead("chan1"); err != nil {
		t.Fatalf("MarkChannelRead() error: %v", err)
	}
	wantPath := "/api/v4/channels/chan1/members/me/view"
	if capturedPath != wantPath {
		t.Errorf("path = %q, want %q", capturedPath, wantPath)
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
}

func TestSearchChannels_OK(t *testing.T) {
	var capturedBody string
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/teams/team1/channels/search" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		writeJSON(w, []map[string]interface{}{
			{"id": "c1", "name": "general", "display_name": "General", "type": "O"},
		})
	}))

	channels, err := client.SearchChannels("team1", "general")
	if err != nil {
		t.Fatalf("SearchChannels() error: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].ID != "c1" {
		t.Errorf("channel.ID = %q, want %q", channels[0].ID, "c1")
	}
	if !strings.Contains(capturedBody, `"term"`) || !strings.Contains(capturedBody, "general") {
		t.Errorf("expected JSON body with term=general, got %q", capturedBody)
	}
}

func TestSearchUsers_OK(t *testing.T) {
	_, client := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/users/search" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, []map[string]string{
			{"id": "u1", "username": "alice"},
		})
	}))

	users, err := client.SearchUsers("ali")
	if err != nil {
		t.Fatalf("SearchUsers() error: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].ID != "u1" {
		t.Errorf("user.ID = %q, want %q", users[0].ID, "u1")
	}
	if users[0].Username != "alice" {
		t.Errorf("user.Username = %q, want %q", users[0].Username, "alice")
	}
}
