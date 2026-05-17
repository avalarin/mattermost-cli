package store

import (
	"testing"
)

func openMemoryDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})
	return db
}

func TestOpenCreatesSchema(t *testing.T) {
	db := openMemoryDB(t)

	// Verify both tables exist by querying sqlite_master.
	tables := map[string]bool{"channels": false, "messages": false}
	rows, err := db.db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tables[name] = true
	}
	for _, tbl := range []string{"channels", "messages"} {
		if !tables[tbl] {
			t.Errorf("table %q not found after Open", tbl)
		}
	}
}

func TestInsertAndGetMessage(t *testing.T) {
	db := openMemoryDB(t)

	msg := Message{
		ID:          "msg-1",
		ChannelID:   "ch-1",
		UserID:      "user-1",
		Text:        "hello",
		SenderName:  "alice",
		ChannelName: "general",
		RootID:      "",
		CreateAt:    1000,
	}
	if err := db.InsertMessage(msg); err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}

	msgs, err := db.GetRecentMessages(10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ID != msg.ID || msgs[0].Text != msg.Text {
		t.Errorf("message mismatch: got %+v", msgs[0])
	}
}

func TestInsertDuplicateIgnored(t *testing.T) {
	db := openMemoryDB(t)

	msg := Message{ID: "dup", ChannelID: "c", UserID: "u", Text: "t", SenderName: "s", ChannelName: "ch", CreateAt: 1}
	if err := db.InsertMessage(msg); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := db.InsertMessage(msg); err != nil {
		t.Errorf("duplicate insert returned error: %v", err)
	}

	msgs, _ := db.GetRecentMessages(10)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message after duplicate insert, got %d", len(msgs))
	}
}

func TestGetRecentMessagesOrdering(t *testing.T) {
	db := openMemoryDB(t)

	for i, ts := range []int64{3000, 1000, 2000} {
		if err := db.InsertMessage(Message{
			ID: string(rune('a' + i)), ChannelID: "c", UserID: "u",
			Text: "msg", SenderName: "s", ChannelName: "ch", CreateAt: ts,
		}); err != nil {
			t.Fatalf("InsertMessage: %v", err)
		}
	}

	msgs, err := db.GetRecentMessages(10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].CreateAt > msgs[1].CreateAt || msgs[1].CreateAt > msgs[2].CreateAt {
		t.Errorf("messages not ordered ascending by create_at: %v %v %v",
			msgs[0].CreateAt, msgs[1].CreateAt, msgs[2].CreateAt)
	}
}

func TestInsertMessagePreservesReplyCount(t *testing.T) {
	db := openMemoryDB(t)

	msg := Message{
		ID: "rc-1", ChannelID: "c", UserID: "u", Text: "hi",
		SenderName: "s", ChannelName: "ch", CreateAt: 1, ReplyCount: 5,
	}
	if err := db.InsertMessage(msg); err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	msgs, err := db.GetRecentMessages(10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected 1 message")
	}
	if msgs[0].ReplyCount != 5 {
		t.Errorf("ReplyCount = %d, want 5", msgs[0].ReplyCount)
	}
}

func TestIncrementReplyCountDB(t *testing.T) {
	db := openMemoryDB(t)

	msg := Message{ID: "root-1", ChannelID: "c", UserID: "u", Text: "root", SenderName: "s", ChannelName: "ch", CreateAt: 1}
	if err := db.InsertMessage(msg); err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}

	if err := db.IncrementReplyCount("root-1"); err != nil {
		t.Fatalf("IncrementReplyCount: %v", err)
	}
	if err := db.IncrementReplyCount("root-1"); err != nil {
		t.Fatalf("IncrementReplyCount second: %v", err)
	}

	got, err := db.GetMessageByID("root-1")
	if err != nil {
		t.Fatalf("GetMessageByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected message, got nil")
	}
	if got.ReplyCount != 2 {
		t.Errorf("ReplyCount = %d, want 2", got.ReplyCount)
	}
}

func TestGetMessageByID(t *testing.T) {
	db := openMemoryDB(t)

	msg := Message{ID: "find-me", ChannelID: "c", UserID: "u", Text: "found", SenderName: "s", ChannelName: "ch", CreateAt: 42}
	if err := db.InsertMessage(msg); err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}

	got, err := db.GetMessageByID("find-me")
	if err != nil {
		t.Fatalf("GetMessageByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected message, got nil")
	}
	if got.Text != "found" {
		t.Errorf("wrong text: %q", got.Text)
	}

	missing, err := db.GetMessageByID("no-such-id")
	if err != nil {
		t.Fatalf("GetMessageByID(missing): %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing ID, got %+v", missing)
	}
}
