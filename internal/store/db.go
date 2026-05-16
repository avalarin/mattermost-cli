package store

import "fmt"

// DB wraps a SQLite database connection.
type DB struct {
	path string
}

// Open opens (or creates) the SQLite database at path.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("database path must not be empty")
	}
	return &DB{path: path}, nil
}
