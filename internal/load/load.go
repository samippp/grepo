// Package load is grepo's only point of contact with the Go toolchain.
//
// It runs `go list` inside the target repo, so grepo sees the code exactly as
// `go build` would: the same go.mod, dependency versions and build tags. Every
// later stage (the package and call graphs now, data-flow analysis later)
// consumes the Result from here, never go/packages directly.
//
// What the target repo needs: it must be a Go module whose dependencies can
// be downloaded. Limitations: only files for the current GOOS/GOARCH are
// loaded, test files are skipped, and nested modules are not followed.
//
// Each package comes back with its parsed syntax trees and type information,
// which the graph package walks to build the call graph. Or Load hands each
// repo package to a Walk as soon as its types are ready, and drops its syntax
// and type information after: a large repo's are too big to hold all at once.
//
// Load is grepo's own loader (golist.go). GoPackages loads the same through
// golang.org/x/tools/go/packages, for vta-diff, which builds SSA from it, and
// for comparing the two.
package load

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log/slog"
	"path/filepath"
	"slices"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/samippp/grepo/internal/logging"
	"github.com/samippp/grepo/internal/store"
)

// Result is a loaded repository.
type Result struct {
	Dir      string // absolute path to the repository root
	Module   string // main module path, empty if none was found
	Packages []*Package
	Checked  []string // import paths type-checked from source, sorted; the rest came from the store
}

// Package is one of the repo's packages, type-checked. After a Walk, Syntax,
// TypesInfo and Share are nil.
type Package struct {
	PkgPath string
	Name    string
	Dir     string
	GoFiles []string
	Imports []string // resolved import paths: vendored ones differ from the source
	Errors  []error

	Fset      *token.FileSet // shared by every package in the Result
	Syntax    []*ast.File    // nil when it came from the store
	Types     *types.Package // from the store: its exported API only, what importers need
	TypesInfo *types.Info    // nil when it came from the store

	// With a store: Key is the package's row, "" if it has none (errors, or
	// types export data can't carry). Share is the graph share saved there;
	// when set, the package came from the store, not from source.
	Key   string
	Share []byte
}

// GoPackages' load mode. Its dependencies' types come from compiled export
// data: slow on a cold build cache, which is why Load doesn't use it.
const mode = packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedModule |
	packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo

// Walk reads one repo package: its syntax and type information when it was
// checked from source, its Share when it came from the store. repo holds
// every repo package's import path; it's shared, so don't change it. Walks
// run concurrently, each while its package's dependents are being checked.
type Walk func(pkg *Package, repo map[string]bool)

// Load loads every package under dir (the equivalent of `go list ./...`).
// Packages that fail to load are logged as warnings and still returned, with
// their Errors field set. With a store, dependencies unchanged since an
// earlier load come from it instead of being type-checked; nil loads
// everything from source. With a walk, it runs on each repo package as soon
// as its types are ready, and Load then drops the package's Syntax,
// TypesInfo and Share; nil keeps them.
func Load(ctx context.Context, dir string, st *store.Store, log *slog.Logger, walk Walk) (*Result, error) {
	return fromGoList(ctx, dir, st, log, walk)
}

// GoPackages loads like Load, through go/packages, and also returns its
// packages. They share syntax and type objects with the Result's, so SSA built
// from them maps back onto a graph built from the Result (vta-diff).
func GoPackages(ctx context.Context, dir string, log *slog.Logger) (*Result, []*packages.Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, err
	}

	// The first load of a repo also downloads its dependencies, which is
	// usually most of the time spent here.
	start := time.Now()
	log.Debug("loading packages", "dir", abs)
	cfg := &packages.Config{Context: ctx, Mode: mode, Dir: abs}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, nil, fmt.Errorf("loading packages in %s: %w", abs, err)
	}
	if len(pkgs) == 0 {
		return nil, nil, fmt.Errorf("no Go packages found in %s", abs)
	}

	res := &Result{Dir: abs}
	for _, p := range pkgs {
		if res.Module == "" && p.Module != nil && p.Module.Main {
			res.Module = p.Module.Path
		}
		for _, e := range p.Errors {
			log.Warn("package has errors", "package", p.PkgPath, "err", e.Error())
		}
		res.Packages = append(res.Packages, wrap(p))
	}
	log.Info("loaded packages", "module", res.Module, "count", len(pkgs), "took", logging.Since(start))
	return res, pkgs, nil
}

// wrap copies what grepo uses from a go/packages package, sharing its syntax
// and type objects.
func wrap(p *packages.Package) *Package {
	pkg := &Package{
		PkgPath:   p.PkgPath,
		Name:      p.Name,
		Dir:       p.Dir,
		GoFiles:   p.GoFiles,
		Fset:      p.Fset,
		Syntax:    p.Syntax,
		Types:     p.Types,
		TypesInfo: p.TypesInfo,
	}
	// Keys are import paths as written in source; values carry the resolved path.
	for _, imp := range p.Imports {
		pkg.Imports = append(pkg.Imports, imp.PkgPath)
	}
	slices.Sort(pkg.Imports)
	for _, e := range p.Errors {
		pkg.Errors = append(pkg.Errors, e)
	}
	return pkg
}
