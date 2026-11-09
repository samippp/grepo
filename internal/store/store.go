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
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure Go: no C compiler, like the loader
)

// schemaVersion is bumped when the table changes. It's a cache, so an old
// table is dropped rather than migrated.
const schemaVersion = 1

const schema = `CREATE TABLE IF NOT EXISTS packages (
	key         TEXT PRIMARY KEY,
	import_path TEXT NOT NULL,
	last_used   INTEGER NOT NULL, -- unix seconds, for pruning
	export      BLOB              -- gcexportdata
)`

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

// DefaultPath is the shared database, under the user cache directory.
func DefaultPath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "grepo", "store.db"), nil
}

// Open opens the database at path, creating it if needed.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// WAL lets runs read while another writes; writers wait for each other
	// (busy_timeout) and take the write lock when their transaction starts.
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening store %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version == schemaVersion {
		return nil
	}
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS packages`,
		schema,
		fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion),
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Export returns the export data cached under key, if any.
func (s *Store) Export(ctx context.Context, key string) ([]byte, bool, error) {
	var export []byte
	err := s.db.QueryRowContext(ctx, `SELECT export FROM packages WHERE key = ?`, key).Scan(&export)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return export, true, nil
}

// Save writes rows, replacing any under the same key, and marks the keys in
// used as used now, in one transaction.
func (s *Store) Save(ctx context.Context, rows []Row, used []string) error {
	if len(rows) == 0 && len(used) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	if len(rows) > 0 {
		put, err := tx.PrepareContext(ctx, `INSERT INTO packages (key, import_path, last_used, export) VALUES (?, ?, ?, ?)
			ON CONFLICT (key) DO UPDATE SET import_path = excluded.import_path, last_used = excluded.last_used, export = excluded.export`)
		if err != nil {
			return err
		}
		defer put.Close()
		for _, r := range rows {
			if _, err := put.ExecContext(ctx, r.Key, r.ImportPath, now, r.Export); err != nil {
				return err
			}
		}
	}
	if len(used) > 0 {
		touch, err := tx.PrepareContext(ctx, `UPDATE packages SET last_used = ? WHERE key = ?`)
		if err != nil {
			return err
		}
		defer touch.Close()
		for _, key := range used {
			if _, err := touch.ExecContext(ctx, now, key); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
