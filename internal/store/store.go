// Package store is grepo's on-disk cache: one shared SQLite database, one row
// per package key (see load's packageKey). A row holds what a later run needs
// so it doesn't type-check the package again: its export data, and later its
// graph share.
//
// Keys say everything a row depends on, so rows are shared by every repo
// using the same module cache, and nothing is ever invalidated: a changed
// package gets a new key.
package store

import (
	"context"
	"database/sql"
	"errors"
)

// Store is an open cache database. Safe for concurrent use, and for several
// processes on one file.
type Store struct {
	db *sql.DB
}

// Row is one package's cached entry.
type Row struct {
	Key        string
	ImportPath string // for debugging and pruning; the key already holds it
	Export     []byte // gcexportdata
}

var errNotImplemented = errors.New("store: not implemented")

// DefaultPath is the shared database, under the user cache directory.
func DefaultPath() (string, error) {
	return "", errNotImplemented
}

// Open opens the database at path, creating it if needed.
func Open(path string) (*Store, error) {
	return nil, errNotImplemented
}

// Close closes the database.
func (s *Store) Close() error {
	return errNotImplemented
}

// Export returns the export data cached under key, if any.
func (s *Store) Export(ctx context.Context, key string) ([]byte, bool, error) {
	return nil, false, errNotImplemented
}

// Save writes rows, replacing any under the same key, and marks the keys in
// used as used now, in one transaction.
func (s *Store) Save(ctx context.Context, rows []Row, used []string) error {
	return errNotImplemented
}
