package mattermost_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
