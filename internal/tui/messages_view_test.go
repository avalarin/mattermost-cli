package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
)

// --- renderMessageLine badge tests ---

func TestReplyCountBadgeRoot(t *testing.T) {
	msg := mattermost.Message{
		ID:         "root-1",
		ChannelID:  "c1",
		UserID:     "u1",
		Text:       "hello",
		CreateAt:   time.Now().UnixMilli(),
		ReplyCount: 3,
	}
	line := renderMessageLine(msg, "alice", "general", "", "02.01.2006", 120, false, false, false)
	if !strings.Contains(line, "⤵︎ 3") {
		t.Errorf("expected ⤵︎ 3 badge in: %q", line)
	}
}

func TestReplyCountBadgeRootZero(t *testing.T) {
	msg := mattermost.Message{
		ID:        "root-2",
		ChannelID: "c1",
		UserID:    "u1",
		Text:      "no replies",
		CreateAt:  time.Now().UnixMilli(),
	}
	line := renderMessageLine(msg, "alice", "general", "", "02.01.2006", 120, false, false, false)
	if strings.Contains(line, "⤵︎") {
		t.Errorf("expected no ⤵︎ badge when ReplyCount=0, got: %q", line)
	}
}

func TestReplyBadgeInAllActivity(t *testing.T) {
	msg := mattermost.Message{
		ID:        "reply-1",
		ChannelID: "c1",
		UserID:    "u1",
		Text:      "a reply",
		CreateAt:  time.Now().UnixMilli(),
		RootID:    "root-1",
	}
	line := renderMessageLine(msg, "bob", "general", "", "02.01.2006", 120, false, true, false)
	if !strings.Contains(line, "⤴︎") {
		t.Errorf("expected ⤴︎ badge for reply in All Activity, got: %q", line)
	}
}

func TestNoReplyBadgeInChannelView(t *testing.T) {
	msg := mattermost.Message{
		ID:       "reply-2",
		ChannelID: "c1",
		UserID:   "u1",
		Text:     "a reply",
		CreateAt: time.Now().UnixMilli(),
		RootID:   "root-1",
	}
	// isAllActivity=false → no ⤴︎ badge
	line := renderMessageLine(msg, "bob", "general", "", "02.01.2006", 120, false, false, false)
	if strings.Contains(line, "⤴︎") {
		t.Errorf("expected no ⤴︎ badge for reply in channel view, got: %q", line)
	}
}

// --- MessagesView.IncrementReplyCount ---

func TestMessagesViewIncrementReplyCount(t *testing.T) {
	mv := NewMessagesView(nil)
	mv = mv.SetSize(120, 20)
	mv = mv.SetAllActivity(true)

	root := feedItem{
		kind: feedItemKindMessage,
		msg: feedMessage{
			post: mattermost.Message{
				ID:         "root-1",
				Text:       "root message",
				CreateAt:   time.Now().UnixMilli(),
				ReplyCount: 0,
			},
			senderName:  "alice",
			channelName: "general",
		},
	}
	mv = mv.SetFeedItems([]feedItem{root})

	if strings.Contains(mv.vp.View(), "⤵︎") {
		t.Error("expected no ⤵︎ badge before IncrementReplyCount")
	}

	mv = mv.IncrementReplyCount("root-1")

	if !strings.Contains(mv.vp.View(), "⤵︎ 1") {
		t.Errorf("expected ⤵︎ 1 badge after IncrementReplyCount, view: %q", mv.vp.View())
	}
}

// --- All Activity shows replies ---

func TestAllActivityShowsReplies(t *testing.T) {
	mv := NewMessagesView(nil)
	mv = mv.SetSize(120, 20)
	mv = mv.SetAllActivity(true)

	items := []feedItem{
		{
			kind: feedItemKindMessage,
			msg: feedMessage{
				post: mattermost.Message{
					ID: "root-1", Text: "root", CreateAt: time.Now().UnixMilli(),
				},
				senderName: "alice", channelName: "general",
			},
		},
		{
			kind: feedItemKindMessage,
			msg: feedMessage{
				post: mattermost.Message{
					ID: "reply-1", Text: "reply text", CreateAt: time.Now().UnixMilli(), RootID: "root-1",
				},
				senderName: "bob", channelName: "general",
			},
		},
	}
	mv = mv.SetFeedItems(items)
	view := mv.vp.View()

	if !strings.Contains(view, "reply text") {
		t.Error("All Activity should show reply messages")
	}
	if !strings.Contains(view, "⤴︎") {
		t.Error("reply in All Activity should have ⤴︎ badge")
	}
}
