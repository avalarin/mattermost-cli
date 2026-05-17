package store

import (
	"strings"
	"testing"
	"time"
)

func openMemoryStore(t *testing.T) *Store {
	t.Helper()
	db := openMemoryDB(t)
	return NewStore(db)
}

// --- render tests (moved from tui/model_test.go) ---

func TestRenderReply(t *testing.T) {
	createAt := time.Now().UnixMilli()
	msg := Message{
		ID:          "reply-id",
		ChannelName: "general",
		SenderName:  "alice",
		Text:        "I am fine, thanks!",
		CreateAt:    createAt,
		RootID:      "parent-id",
	}

	line := renderMessage(msg, "Hello everyone, how are you doing today?")

	if !strings.Contains(line, "↩") {
		t.Errorf("expected ↩ in line, got: %q", line)
	}
	if !strings.Contains(line, "alice") {
		t.Errorf("expected sender name in line, got: %q", line)
	}
	if !strings.Contains(line, "I am fine, thanks!") {
		t.Errorf("expected message text in line, got: %q", line)
	}
	if !strings.Contains(line, "Hello everyone") {
		t.Errorf("expected parent snippet in line, got: %q", line)
	}
}

func TestRenderReplyNoParent(t *testing.T) {
	msg := Message{
		ID:          "reply-id",
		ChannelName: "general",
		SenderName:  "bob",
		Text:        "my reply",
		CreateAt:    time.Now().UnixMilli(),
		RootID:      "unknown-parent",
	}

	line := renderMessage(msg, "")

	if !strings.Contains(line, "↩") {
		t.Errorf("expected ↩ indicator even without parent snippet, got: %q", line)
	}
	if !strings.Contains(line, "my reply") {
		t.Errorf("expected message text in line, got: %q", line)
	}
	if strings.Contains(line, "В ответ на") {
		t.Errorf("expected no parent snippet when snippet is empty, got: %q", line)
	}
}

func TestRenderNormalMessage(t *testing.T) {
	msg := Message{
		ID:          "msg-id",
		ChannelName: "random",
		SenderName:  "charlie",
		Text:        "hello world",
		CreateAt:    time.Now().UnixMilli(),
	}

	line := renderMessage(msg, "")

	if strings.Contains(line, "↩") {
		t.Errorf("expected no ↩ for top-level message, got: %q", line)
	}
	if !strings.Contains(line, "charlie") {
		t.Errorf("expected sender name in line, got: %q", line)
	}
	if !strings.Contains(line, "hello world") {
		t.Errorf("expected message text in line, got: %q", line)
	}
	if !strings.Contains(line, "#random") {
		t.Errorf("expected channel name in line, got: %q", line)
	}
}

// --- store tests ---

func TestGetParentSnippetFound(t *testing.T) {
	s := openMemoryStore(t)

	parent := Message{ID: "p1", Text: "Hello everyone, how are you doing today?", SenderName: "a", ChannelName: "ch", CreateAt: 1}
	s.AddMessage(parent)

	snippet := s.GetParentSnippet("p1")
	if snippet == "" {
		t.Fatal("expected non-empty snippet")
	}
	if !strings.HasPrefix(snippet, "Hello everyone") {
		t.Errorf("unexpected snippet: %q", snippet)
	}
}

func TestGetParentSnippetFromDB(t *testing.T) {
	db := openMemoryDB(t)

	// Insert parent directly to DB, bypassing the in-memory store.
	parent := Message{
		ID: "db-parent", Text: "This message is only in DB", ChannelID: "c",
		UserID: "u", SenderName: "s", ChannelName: "ch", CreateAt: 100,
	}
	if err := db.InsertMessage(parent); err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}

	// Create a fresh store — its in-memory list is empty.
	s := NewStore(db)

	snippet := s.GetParentSnippet("db-parent")
	if snippet == "" {
		t.Fatal("expected snippet from DB, got empty")
	}
	if !strings.Contains(snippet, "This message is only in DB") {
		t.Errorf("unexpected snippet from DB: %q", snippet)
	}
}

func TestAddMessageCap(t *testing.T) {
	s := openMemoryStore(t)

	for i := range messageCap + 10 {
		s.AddMessage(Message{
			ID:          string(rune(i + 1)),
			Text:        "msg",
			SenderName:  "u",
			ChannelName: "ch",
			CreateAt:    int64(i),
		})
	}

	s.mu.Lock()
	n := len(s.messages)
	s.mu.Unlock()

	if n > messageCap {
		t.Errorf("in-memory store exceeded cap: %d > %d", n, messageCap)
	}
}

func TestLoadRecent(t *testing.T) {
	db := openMemoryDB(t)

	for i := range 5 {
		if err := db.InsertMessage(Message{
			ID: string(rune('a' + i)), ChannelID: "c", UserID: "u",
			Text: "msg", SenderName: "s", ChannelName: "ch", CreateAt: int64(i + 1),
		}); err != nil {
			t.Fatalf("InsertMessage: %v", err)
		}
	}

	s := NewStore(db)
	lines, err := s.LoadRecent(3)
	if err != nil {
		t.Fatalf("LoadRecent: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}
