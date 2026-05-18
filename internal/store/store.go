package store

import (
	"fmt"
	"sync"
	"time"
)

const messageCap = 1000
const channelMessageCap = 500

// Message is a fully-resolved message ready for display and persistence.
// It extends the raw API fields with sender_name and channel_name resolved at
// ingest time from the WebSocket event envelope.
type Message struct {
	ID          string
	ChannelID   string
	UserID      string
	Text        string
	CreateAt    int64
	RootID      string
	SenderName  string
	ChannelName string
	ReplyCount  int
}

// Store holds an in-memory message list (capped at 1000) backed by SQLite.
type Store struct {
	mu              sync.Mutex
	db              *DB
	messages        []Message
	channelMessages map[string][]Message // channelID → messages (oldest first, cap 500)
}

// NewStore creates a new Store backed by the given DB.
func NewStore(db *DB) *Store {
	return &Store{
		db:              db,
		messages:        make([]Message, 0, messageCap),
		channelMessages: make(map[string][]Message),
	}
}

// AddMessage stores the message (in memory and DB) and returns its rendered display line.
func (s *Store) AddMessage(msg Message) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	snippet := s.getParentSnippetLocked(msg.RootID)
	line := renderMessage(msg, snippet)

	if len(s.messages) >= messageCap {
		s.messages = s.messages[1:]
	}
	s.messages = append(s.messages, msg)

	if s.db != nil {
		// Best-effort: DB write errors are not fatal for live display.
		_ = s.db.InsertMessage(msg)
	}

	return line
}

// GetParentSnippet returns the first 40 runes of the parent message's text,
// searching in-memory first then falling back to the database.
func (s *Store) GetParentSnippet(rootID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getParentSnippetLocked(rootID)
}

func (s *Store) getParentSnippetLocked(rootID string) string {
	if rootID == "" {
		return ""
	}
	for i := len(s.messages) - 1; i >= 0; i-- {
		if s.messages[i].ID == rootID {
			return truncate(s.messages[i].Text, 40)
		}
	}
	if s.db != nil {
		if msg, err := s.db.GetMessageByID(rootID); err == nil && msg != nil {
			return truncate(msg.Text, 40)
		}
	}
	return ""
}

// LoadRecent loads up to limit recent messages from the database into the in-memory
// store and returns them as raw Message values. Intended for startup history load.
func (s *Store) LoadRecent(limit int) ([]Message, error) {
	if s.db == nil {
		return nil, nil
	}
	msgs, err := s.db.GetRecentMessages(limit)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, msg := range msgs {
		if len(s.messages) >= messageCap {
			s.messages = s.messages[1:]
		}
		s.messages = append(s.messages, msg)
	}
	return msgs, nil
}

// AddChannelMessages adds messages to the per-channel cache.
// prepend=true inserts older messages at the front (infinite scroll).
// prepend=false appends newer messages at the back (initial load / WS events).
// Cap per channel: 500 messages total. Also persists each message to the DB (best-effort).
func (s *Store) AddChannelMessages(channelID string, msgs []Message, prepend bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.channelMessages[channelID]

	// Build an ID set from the incoming batch so we can deduplicate.
	// Incoming msgs take precedence (they carry up-to-date reply_count from REST).
	incomingIDs := make(map[string]struct{}, len(msgs))
	for _, m := range msgs {
		incomingIDs[m.ID] = struct{}{}
	}
	filtered := existing[:0:0] // same backing type, zero length
	for _, m := range existing {
		if _, dup := incomingIDs[m.ID]; !dup {
			filtered = append(filtered, m)
		}
	}

	var combined []Message
	if prepend {
		combined = append(msgs, filtered...)
	} else {
		combined = append(filtered, msgs...)
	}
	// Cap to channelMessageCap, keeping the appropriate end.
	// When prepending (older msgs), keep the front (oldest); when appending, keep the tail (newest).
	if len(combined) > channelMessageCap {
		if prepend {
			combined = combined[:channelMessageCap]
		} else {
			combined = combined[len(combined)-channelMessageCap:]
		}
	}
	s.channelMessages[channelID] = combined

	if s.db != nil {
		for _, msg := range msgs {
			_ = s.db.InsertMessage(msg)
		}
	}
}

// GetChannelMessages returns all messages for a channel in order (oldest first).
func (s *Store) GetChannelMessages(channelID string) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.channelMessages[channelID]
	if len(msgs) == 0 {
		return nil
	}
	result := make([]Message, len(msgs))
	copy(result, msgs)
	return result
}

// IncrementReplyCount increments the reply_count of the message with the given ID
// in both the global in-memory list and all per-channel caches, then persists to the DB.
func (s *Store) IncrementReplyCount(rootID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.messages {
		if s.messages[i].ID == rootID {
			s.messages[i].ReplyCount++
			break
		}
	}

	for channelID, msgs := range s.channelMessages {
		for i := range msgs {
			if msgs[i].ID == rootID {
				s.channelMessages[channelID][i].ReplyCount++
				break
			}
		}
	}

	if s.db != nil {
		_ = s.db.IncrementReplyCount(rootID)
	}
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max]) + "..."
	}
	return s
}

// renderMessage formats a single message for display.
// Used by AddMessage to generate the stored rendered form.
func renderMessage(msg Message, snippet string) string {
	ts := time.UnixMilli(msg.CreateAt).Format("15:04")

	if msg.RootID != "" {
		if snippet != "" {
			return fmt.Sprintf("[%s] #%s  ↩ @%s: %s  (%s)", ts, msg.ChannelName, msg.SenderName, msg.Text, snippet)
		}
		return fmt.Sprintf("[%s] #%s  ↩ @%s: %s", ts, msg.ChannelName, msg.SenderName, msg.Text)
	}
	return fmt.Sprintf("[%s] #%s  @%s: %s", ts, msg.ChannelName, msg.SenderName, msg.Text)
}
