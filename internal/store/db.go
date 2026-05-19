package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB wraps a SQLite database connection.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and initialises the schema.
// path can be a file path or a SQLite URI (e.g. "file::memory:?cache=shared").
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("database path must not be empty")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	if err := createSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &DB{db: db}, nil
}

// Close releases the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

func createSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS channels (
			id   TEXT PRIMARY KEY,
			name TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS messages (
			id           TEXT PRIMARY KEY,
			channel_id   TEXT NOT NULL,
			user_id      TEXT NOT NULL,
			text         TEXT NOT NULL,
			sender_name  TEXT NOT NULL,
			channel_name TEXT NOT NULL,
			root_id      TEXT NOT NULL DEFAULT '',
			create_at    INTEGER NOT NULL,
			reply_count  INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_messages_create_at ON messages(create_at DESC);
	`)
	if err != nil {
		return err
	}
	// Migration for existing databases: add reply_count column if not present.
	// SQLite returns an error if the column already exists — we ignore it.
	_, _ = db.Exec(`ALTER TABLE messages ADD COLUMN reply_count INTEGER NOT NULL DEFAULT 0`)
	return nil
}

// InsertMessage inserts a message, updating reply_count if the ID already exists.
func (d *DB) InsertMessage(msg Message) error {
	_, err := d.db.Exec(`
		INSERT INTO messages (id, channel_id, user_id, text, sender_name, channel_name, root_id, create_at, reply_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET reply_count = EXCLUDED.reply_count
	`, msg.ID, msg.ChannelID, msg.UserID, msg.Text, msg.SenderName, msg.ChannelName, msg.RootID, msg.CreateAt, msg.ReplyCount)
	return err
}

// GetRecentMessages returns up to limit of the most recent messages, ordered oldest-first.
func (d *DB) GetRecentMessages(limit int) ([]Message, error) {
	rows, err := d.db.Query(`
		SELECT id, channel_id, user_id, text, sender_name, channel_name, root_id, create_at, reply_count
		FROM (
			SELECT id, channel_id, user_id, text, sender_name, channel_name, root_id, create_at, reply_count
			FROM messages
			ORDER BY create_at DESC
			LIMIT ?
		)
		ORDER BY create_at ASC
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Text, &m.SenderName, &m.ChannelName, &m.RootID, &m.CreateAt, &m.ReplyCount); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// GetMessageByID returns the message with the given ID, or nil if not found.
func (d *DB) GetMessageByID(id string) (*Message, error) {
	row := d.db.QueryRow(`
		SELECT id, channel_id, user_id, text, sender_name, channel_name, root_id, create_at, reply_count
		FROM messages WHERE id = ?
	`, id)
	var m Message
	err := row.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Text, &m.SenderName, &m.ChannelName, &m.RootID, &m.CreateAt, &m.ReplyCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// IncrementReplyCount increments the reply_count of the message with the given ID.
func (d *DB) IncrementReplyCount(id string) error {
	_, err := d.db.Exec(`UPDATE messages SET reply_count = reply_count + 1 WHERE id = ?`, id)
	return err
}

// PruneMessages removes all but the most recent keepRecent messages from the database.
// This keeps the database from growing unboundedly across sessions.
func (d *DB) PruneMessages(keepRecent int) error {
	_, err := d.db.Exec(`
		DELETE FROM messages
		WHERE id NOT IN (
			SELECT id FROM messages ORDER BY create_at DESC LIMIT ?
		)
	`, keepRecent)
	return err
}

// DeleteAllMessages removes all rows from the messages table.
func (d *DB) DeleteAllMessages() error {
	_, err := d.db.Exec(`DELETE FROM messages`)
	return err
}

// GetMessageStats returns the total count of stored messages and the oldest/newest
// create_at timestamps (Unix milliseconds). Returns zeros when the table is empty.
func (d *DB) GetMessageStats() (count int, minCreateAt, maxCreateAt int64, err error) {
	row := d.db.QueryRow(`
		SELECT COUNT(*), COALESCE(MIN(create_at), 0), COALESCE(MAX(create_at), 0)
		FROM messages
	`)
	err = row.Scan(&count, &minCreateAt, &maxCreateAt)
	return
}
