package store

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// A saved row comes back by its key, after the database is closed and
// reopened too; another key gives nothing.
func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	s := open(t, path)
	if err := s.Save(t.Context(), []Row{{Key: "k1", ImportPath: "example.com/a", Export: []byte("types")}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = open(t, path)
	got, ok, err := s.Export(t.Context(), "k1")
	if err != nil || !ok || !bytes.Equal(got, []byte("types")) {
		t.Errorf("Export(k1) = %q, %v, %v; want types, true, nil", got, ok, err)
	}
	if got, ok, err := s.Export(t.Context(), "k2"); err != nil || ok {
		t.Errorf("Export(k2) = %q, %v, %v; want nothing", got, ok, err)
	}
}

// Saving under a key that's already there replaces the row.
func TestSaveReplaces(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "store.db"))
	for _, export := range []string{"old", "new"} {
		if err := s.Save(t.Context(), []Row{{Key: "k", ImportPath: "a", Export: []byte(export)}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got, _, _ := s.Export(t.Context(), "k"); string(got) != "new" {
		t.Errorf("Export(k) = %q, want new", got)
	}
}

// Saving marks used keys as used now, for pruning by last use later.
func TestSaveMarksUsed(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "store.db"))
	if err := s.Save(t.Context(), []Row{{Key: "k", ImportPath: "a", Export: []byte("x")}}, nil); err != nil {
		t.Fatal(err)
	}
	lastUsed := func() int64 {
		var at int64
		if err := s.db.QueryRowContext(t.Context(), `SELECT last_used FROM packages WHERE key = ?`, "k").Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	if _, err := s.db.ExecContext(t.Context(), `UPDATE packages SET last_used = 1 WHERE key = ?`, "k"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(t.Context(), nil, []string{"k"}); err != nil {
		t.Fatal(err)
	}
	if at := lastUsed(); at <= 1 {
		t.Errorf("last_used = %d after Save marked it used, want now", at)
	}
}

// Two runs share the database: saves from two connections at once all land.
func TestConcurrentSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	stores := []*Store{open(t, path), open(t, path)}
	var wg sync.WaitGroup
	for i, s := range stores {
		wg.Go(func() {
			for j := range 20 {
				key := fmt.Sprintf("k%d-%d", i, j)
				if err := s.Save(t.Context(), []Row{{Key: key, ImportPath: "a", Export: []byte(key)}}, nil); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	for i := range stores {
		for j := range 20 {
			key := fmt.Sprintf("k%d-%d", i, j)
			if got, ok, err := stores[0].Export(t.Context(), key); err != nil || !ok || string(got) != key {
				t.Errorf("Export(%s) = %q, %v, %v", key, got, ok, err)
			}
		}
	}
}
