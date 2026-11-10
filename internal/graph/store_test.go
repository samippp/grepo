package graph

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/samippp/grepo/internal/load"
	"github.com/samippp/grepo/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// loadAndBuild loads testdata/simple through st, builds its graph and saves
// the shares, as analyze does.
func loadAndBuild(t *testing.T, st *store.Store) (*load.Result, []byte) {
	t.Helper()
	res, err := load.Load(t.Context(), "testdata/simple", st, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	g, shares, err := BuildCallGraph(res)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveShares(t.Context(), shares); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	return res, data
}

// The second load takes every repo package from the store, and its graph is
// the one walked from source.
func TestCallGraphFromStore(t *testing.T) {
	want, err := json.Marshal(buildFixtureCallGraph(t))
	if err != nil {
		t.Fatal(err)
	}
	st := openStore(t)
	_, cold := loadAndBuild(t, st)
	res, warm := loadAndBuild(t, st)

	for _, p := range res.Packages {
		if p.Share == nil || slices.Contains(res.Checked, p.PkgPath) {
			t.Errorf("%s was checked again", p.PkgPath)
		}
	}
	if !bytes.Equal(cold, want) {
		t.Error("the graph built while filling the store differs from one without it")
	}
	if !bytes.Equal(warm, want) {
		t.Error("the graph built from the store differs from the one walked")
	}
}

// A share the store holds but can't be decoded fails the build, naming the
// package: there's no syntax to walk instead.
func TestCallGraphBadShare(t *testing.T) {
	st := openStore(t)
	res, _ := loadAndBuild(t, st)
	key := res.Packages[0].Key
	if err := st.SaveShares(t.Context(), map[string][]byte{key: []byte("{not json")}); err != nil {
		t.Fatal(err)
	}
	res, err := load.Load(t.Context(), "testdata/simple", st, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = BuildCallGraph(res)
	if err == nil || !strings.Contains(err.Error(), res.Packages[0].PkgPath) {
		t.Errorf("err = %v, want one naming %s", err, res.Packages[0].PkgPath)
	}
}
