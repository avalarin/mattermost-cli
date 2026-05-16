package store

// Store holds in-memory application state.
type Store struct {
	db *DB
}

// NewStore creates a new in-memory store backed by the given DB.
func NewStore(db *DB) *Store {
	return &Store{db: db}
}
