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

// A share is saved with its row, or later by key; saving the row again
// without one keeps it. A key with no row gets no share.
func TestShares(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "store.db"))
	ctx := t.Context()
	share := func(key string) string {
		t.Helper()
		got, ok, err := s.Share(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return "<none>"
		}
		return string(got)
	}
	if err := s.Save(ctx, []Row{{Key: "a", ImportPath: "a", Export: []byte("x")}, {Key: "b", ImportPath: "b", Export: []byte("x"), Share: []byte("b1")}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := share("a"); got != "<none>" {
		t.Errorf("share(a) = %s before one was saved", got)
	}
	if got := share("b"); got != "b1" {
		t.Errorf("share(b) = %s, want b1", got)
	}

	if err := s.SaveShares(ctx, map[string][]byte{"a": []byte("a1"), "missing": []byte("m")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, []Row{{Key: "a", ImportPath: "a", Export: []byte("y")}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := share("a"); got != "a1" {
		t.Errorf("share(a) = %s after the row was saved again without one, want a1", got)
	}
	if got, _, _ := s.Export(ctx, "a"); string(got) != "y" {
		t.Errorf("Export(a) = %s, want the new y", got)
	}
	if got := share("missing"); got != "<none>" {
		t.Errorf("share(missing) = %s: saved without a row", got)
	}
}

// A database from an older schema is dropped, not migrated: it's a cache.
func TestOldSchemaDropped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	s := open(t, path)
	if err := s.Save(t.Context(), []Row{{Key: "k", ImportPath: "a", Export: []byte("x")}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion-1)); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = open(t, path)
	if _, ok, err := s.Export(t.Context(), "k"); err != nil || ok {
		t.Errorf("Export(k) = %v, %v after reopening an old schema; want nothing", ok, err)
	}
}
