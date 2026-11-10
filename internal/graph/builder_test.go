package graph

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"weak"

	"github.com/samippp/grepo/internal/load"
)

// walkFixture builds testdata/simple's graph with a CallGraphBuilder during
// the load, and returns weak pointers to the repo syntax it walked: files,
// and closures, which a graph keeping closure IDs would hold.
func walkFixture(t *testing.T) (*CallGraph, []weak.Pointer[ast.File], []weak.Pointer[ast.FuncLit]) {
	t.Helper()
	root, err := filepath.Abs("testdata/simple")
	if err != nil {
		t.Fatal(err)
	}
	cb := NewCallGraphBuilder(root)
	var mu sync.Mutex
	var files []weak.Pointer[ast.File]
	var lits []weak.Pointer[ast.FuncLit]
	_, err = load.Load(t.Context(), root, nil, slog.New(slog.DiscardHandler), func(pkg *load.Package, repo map[string]bool) {
		cb.Add(pkg, repo)
		mu.Lock()
		defer mu.Unlock()
		for _, f := range pkg.Syntax {
			files = append(files, weak.Make(f))
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					lits = append(lits, weak.Make(lit))
				}
				return true
			})
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := cb.CallGraph()
	if err != nil {
		t.Fatal(err)
	}
	return g, files, lits
}

// Walking each package during the load builds the same graph as walking
// them all after.
func TestCallGraphBuilderMatchesBuildCallGraph(t *testing.T) {
	want, err := json.Marshal(buildFixtureCallGraph(t))
	if err != nil {
		t.Fatal(err)
	}
	cg, _, _ := walkFixture(t)
	got, err := json.Marshal(cg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("the graph built during the load differs from BuildCallGraph's")
	}
}

// The graph holds nothing of the syntax it walked, so the GC can free it.
func TestCallGraphBuilderKeepsNoSyntax(t *testing.T) {
	cg, files, lits := walkFixture(t)
	if len(files) == 0 || len(lits) == 0 {
		t.Fatalf("walked %d files, %d closures; want some of each", len(files), len(lits))
	}
	runtime.GC()
	for _, f := range files {
		if f.Value() != nil {
			t.Errorf("file of package %s still reachable after the walk", f.Value().Name.Name)
		}
	}
	for _, lit := range lits {
		if lit.Value() != nil {
			t.Errorf("closure at %d still reachable after the walk", lit.Value().Pos())
		}
	}
	runtime.KeepAlive(cg)
}
