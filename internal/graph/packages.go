// Package graph turns loaded packages into the graphs grepo displays.
//
// Every function here is a pure transformation: no disk access, no go
// toolchain, and sorted, deterministic output. That keeps the code easy to test
// and keeps the JSON stable across runs, which the LLM cache (M4) relies on.
// For the same reason nothing here logs; callers time and log these builders.
//
// Builders:
//   - BuildPackageGraph (this file, M0): which packages import which.
//
// Planned:
//   - M1: VTA call graph, and the types that cross package boundaries.
//   - M5: flows traced from entry points.
package graph

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samippp/grepo/internal/load"
)

// PackageGraph is the package-level import graph of a repository.
type PackageGraph struct {
	Module   string    `json:"module"`
	Root     string    `json:"root"`
	Packages []Package `json:"packages"`
	Edges    []Edge    `json:"edges"` // imports between packages in this repo only
	Errors   []string  `json:"errors,omitempty"`
}

// Package is one package in the repository.
type Package struct {
	Path    string   `json:"path"`
	Name    string   `json:"name"`
	Dir     string   `json:"dir"`   // relative to Root, slash-separated
	Files   []string `json:"files"` // relative to Root, slash-separated
	Imports Imports  `json:"imports"`
}

// Imports splits a package's imports by where they come from.
type Imports struct {
	Internal []string `json:"internal"`
	External []string `json:"external"`
	Stdlib   []string `json:"stdlib"`
}

// Edge means package From imports package To.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// BuildPackageGraph turns loaded packages into a PackageGraph. Output is sorted
// so repeated runs produce identical JSON.
func BuildPackageGraph(res *load.Result) *PackageGraph {
	internal := make(map[string]bool, len(res.Packages))
	for _, p := range res.Packages {
		internal[p.PkgPath] = true
	}

	g := &PackageGraph{
		Module:   res.Module,
		Root:     filepath.ToSlash(res.Dir),
		Packages: []Package{},
		Edges:    []Edge{},
	}

	for _, p := range res.Packages {
		for _, e := range p.Errors {
			g.Errors = append(g.Errors, e.Error())
		}

		pkg := Package{
			Path:  p.PkgPath,
			Name:  p.Name,
			Dir:   rel(res.Dir, p.Dir),
			Files: []string{},
			Imports: Imports{
				Internal: []string{},
				External: []string{},
				Stdlib:   []string{},
			},
		}
		for _, f := range p.GoFiles {
			pkg.Files = append(pkg.Files, rel(res.Dir, f))
		}

		// Keys are import paths as written in source; values carry the resolved
		// path, which differs for vendored packages.
		for _, imp := range p.Imports {
			path := imp.PkgPath
			switch {
			case internal[path]:
				pkg.Imports.Internal = append(pkg.Imports.Internal, path)
				g.Edges = append(g.Edges, Edge{From: p.PkgPath, To: path})
			case isStdlib(path):
				pkg.Imports.Stdlib = append(pkg.Imports.Stdlib, path)
			default:
				pkg.Imports.External = append(pkg.Imports.External, path)
			}
		}
		slices.Sort(pkg.Files)
		slices.Sort(pkg.Imports.Internal)
		slices.Sort(pkg.Imports.External)
		slices.Sort(pkg.Imports.Stdlib)

		g.Packages = append(g.Packages, pkg)
	}

	slices.SortFunc(g.Packages, func(a, b Package) int { return cmp.Compare(a.Path, b.Path) })
	slices.SortFunc(g.Edges, func(a, b Edge) int {
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To))
	})
	return g
}

// isStdlib reports whether an import path belongs to the standard library.
// Stdlib paths have no dot in their first element; module paths do.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func rel(root, path string) string {
	if path == "" {
		return ""
	}
	r, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(r)
}
