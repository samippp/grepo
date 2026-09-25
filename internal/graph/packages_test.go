// packages_test.go checks BuildPackageGraph against testdata/simple, a tiny
// real module (main → store → model). The fixture is loaded through load.Load
// rather than mocked, so this also tests the go/packages integration end to
// end.

package graph

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/samippp/grepo/internal/load"
)

func TestBuildPackageGraph(t *testing.T) {
	res, err := load.Load(context.Background(), "testdata/simple", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	g := BuildPackageGraph(res)

	if g.Module != "example.com/simple" {
		t.Errorf("module = %q, want example.com/simple", g.Module)
	}
	if len(g.Errors) > 0 {
		t.Errorf("unexpected load errors: %v", g.Errors)
	}

	wantEdges := []Edge{
		{From: "example.com/simple", To: "example.com/simple/store"},
		{From: "example.com/simple/store", To: "example.com/simple/model"},
	}
	if !slices.Equal(g.Edges, wantEdges) {
		t.Errorf("edges = %v, want %v", g.Edges, wantEdges)
	}

	var store *Package
	for i := range g.Packages {
		if g.Packages[i].Path == "example.com/simple/store" {
			store = &g.Packages[i]
		}
	}
	if store == nil {
		t.Fatal("store package missing")
	}
	if store.Dir != "store" {
		t.Errorf("store dir = %q, want store", store.Dir)
	}
	if !slices.Equal(store.Imports.Stdlib, []string{"sync"}) {
		t.Errorf("store stdlib imports = %v, want [sync]", store.Imports.Stdlib)
	}
}
