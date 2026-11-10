package graph

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"log/slog"
	"os"
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

// ways are the two ways to build a graph: from the Result after the load,
// or during it with a CallGraphBuilder as the walk.
var ways = []struct {
	name   string
	during bool
}{{"after the load", false}, {"during the load", true}}

// build loads the repo at dir through st (nil: no store) and builds its graph.
func build(t *testing.T, dir string, st *store.Store, during bool) (*load.Result, *CallGraph, map[string][]byte, error) {
	t.Helper()
	var cb *CallGraphBuilder
	var walk load.Walk
	if during {
		root, err := filepath.Abs(dir)
		if err != nil {
			t.Fatal(err)
		}
		cb = NewCallGraphBuilder(root)
		walk = cb.Add
	}
	res, err := load.Load(t.Context(), dir, st, slog.New(slog.DiscardHandler), walk)
	if err != nil {
		t.Fatal(err)
	}
	var g *CallGraph
	var shares map[string][]byte
	if during {
		g, shares, err = cb.CallGraph()
	} else {
		g, shares, err = BuildCallGraph(res)
	}
	return res, g, shares, err
}

// loadAndBuild loads testdata/simple through st, builds its graph and saves
// the shares, as analyze does.
func loadAndBuild(t *testing.T, st *store.Store, during bool) (*load.Result, []byte) {
	t.Helper()
	return buildDir(t, "testdata/simple", st, during)
}

// buildDir is loadAndBuild for the repo at dir; a nil st builds without one.
func buildDir(t *testing.T, dir string, st *store.Store, during bool) (*load.Result, []byte) {
	t.Helper()
	res, g, shares, err := build(t, dir, st, during)
	if err != nil {
		t.Fatal(err)
	}
	if st != nil {
		if err := st.SaveShares(t.Context(), shares); err != nil {
			t.Fatal(err)
		}
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
	for _, way := range ways {
		t.Run(way.name, func(t *testing.T) {
			st := openStore(t)
			_, cold := loadAndBuild(t, st, way.during)
			res, warm := loadAndBuild(t, st, way.during)

			if checked := repoChecked(res); len(checked) > 0 {
				t.Errorf("checked %v again", checked)
			}
			if !bytes.Equal(cold, want) {
				t.Error("the graph built while filling the store differs from one without it")
			}
			if !bytes.Equal(warm, want) {
				t.Error("the graph built from the store differs from the one walked")
			}
		})
	}
}

// A share the store holds but can't be decoded fails the build, naming the
// package: there's no syntax to walk instead.
func TestCallGraphBadShare(t *testing.T) {
	for _, way := range ways {
		t.Run(way.name, func(t *testing.T) {
			st := openStore(t)
			res, _ := loadAndBuild(t, st, way.during)
			bad := res.Packages[0]
			if err := st.SaveShares(t.Context(), map[string][]byte{bad.Key: []byte("{not json")}); err != nil {
				t.Fatal(err)
			}
			_, _, _, err := build(t, "testdata/simple", st, way.during)
			if err == nil || !strings.Contains(err.Error(), bad.PkgPath) {
				t.Errorf("err = %v, want one naming %s", err, bad.PkgPath)
			}
		})
	}
}

// copySimple copies testdata/simple to a temp dir, so tests can edit it.
func copySimple(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir("testdata/simple", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("testdata/simple", path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// repoChecked is the repo packages res checked from source, sorted.
func repoChecked(res *load.Result) []string {
	var paths []string
	for _, p := range res.Packages {
		if slices.Contains(res.Checked, p.PkgPath) {
			paths = append(paths, p.PkgPath)
		}
	}
	slices.Sort(paths)
	return paths
}

// Each set of repo packages walked while the others come from the store
// builds the graph walked from source. A package walked this way imports
// its repo imports' types from export data: model's unexported seal method
// that store's Sealer gets by embedding, its alias ID and const MaxTag.
func TestCallGraphPartlyFromStore(t *testing.T) {
	want, err := json.Marshal(buildFixtureCallGraph(t))
	if err != nil {
		t.Fatal(err)
	}
	pkgs := []string{mainPkg, modelPkg, storePkg}
	for set := 1; set < 1<<len(pkgs); set++ {
		var walk []string
		for i, p := range pkgs {
			if set&(1<<i) != 0 {
				walk = append(walk, p)
			}
		}
		for _, way := range ways {
			t.Run(strings.Join(walk, "+")+", "+way.name, func(t *testing.T) {
				t.Parallel() // each has its own store
				st := openStore(t)
				cold, _ := loadAndBuild(t, st, way.during)
				drop := map[string][]byte{}
				for _, p := range cold.Packages {
					if slices.Contains(walk, p.PkgPath) {
						drop[p.Key] = nil
					}
				}
				if err := st.SaveShares(t.Context(), drop); err != nil {
					t.Fatal(err)
				}
				res, got := loadAndBuild(t, st, way.during)
				if checked := repoChecked(res); !slices.Equal(checked, slices.Sorted(slices.Values(walk))) {
					t.Errorf("checked %v, want %v", checked, walk)
				}
				if !bytes.Equal(got, want) {
					t.Error("the graph differs from the one walked from source")
				}
			})
		}
	}
}

func editFile(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s doesn't contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// After each edit, a build through the store matches one without it, and
// checks only the edited packages and those importing them.
func TestCallGraphFromStoreAfterEdits(t *testing.T) {
	all := []string{mainPkg, modelPkg, storePkg}
	for _, tc := range []struct {
		name    string
		edit    func(t *testing.T, dir string)
		checked []string // repo packages checked from source after the edit
	}{
		{"main body", func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "main.go"), `greet("closure")`, `greet("closure, edited")`)
		}, []string{mainPkg}},
		{"store body", func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "store/store.go"), "return s.users[id]", "u := s.users[id]\n\treturn u")
		}, []string{mainPkg, storePkg}},
		{"model body", func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "model/kinds.go"), `return "user"`, `return "user, edited"`)
		}, all},
		{"model field", func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "model/model.go"), "Name string", "Name  string\n\tEmail string")
		}, all},
		{"store type now implements a model interface", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "store/named.go"), "package store\n\nfunc (Fake) Name() string { return \"fake\" }\n")
		}, []string{mainPkg, storePkg}},
		{"model type now implements a store interface", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "model/getter.go"), "package model\n\ntype Repo struct{}\n\nfunc (Repo) Get(id int) User { return User{} }\n")
		}, all},
		{"removed file", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "store/alias.go")); err != nil {
				t.Fatal(err)
			}
		}, []string{mainPkg, storePkg}},
		{"new repo import", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "users.go"), "package main\n\nimport \"example.com/simple/model\"\n\nvar admin = model.User{Name: \"admin\"}\n")
		}, []string{mainPkg}},
		{"file excluded by build tags", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "store/never.go"), "//go:build never\n\npackage store\n\nfunc Never() {}\n")
		}, nil},
	} {
		for _, way := range ways {
			t.Run(tc.name+", "+way.name, func(t *testing.T) {
				t.Parallel() // each has its own copy and store
				dir := copySimple(t)
				st := openStore(t)
				buildDir(t, dir, st, way.during)
				tc.edit(t, dir)

				res, got := buildDir(t, dir, st, way.during)
				if checked := repoChecked(res); !slices.Equal(checked, tc.checked) {
					t.Errorf("checked %v, want %v", checked, tc.checked)
				}
				for _, p := range res.Packages {
					if len(p.Errors) > 0 {
						t.Fatalf("the edit broke %s: %v", p.PkgPath, p.Errors)
					}
				}
				_, want := buildDir(t, dir, nil, false)
				if !bytes.Equal(got, want) {
					t.Error("the graph built through the store differs from one without it")
				}
			})
		}
	}
}
