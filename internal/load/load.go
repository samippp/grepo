// Package load is grepo's only contact with the Go toolchain. It runs `go
// list` in the target module, so grepo sees the code as `go build` does, then
// parses and type-checks it. Later stages read its Result, never go/packages.
//
// The target must be a module whose dependencies can be downloaded. Only the
// current GOOS/GOARCH's files are loaded; tests and nested modules are skipped.
//
// Load (golist.go) can hand each repo package to a Walk once its types are
// ready, then drop its syntax: a large repo's is too big to hold at once.
// GoPackages loads through go/packages instead, for vta-diff's SSA.
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
	Types     *types.Package // from the store: checked from source without bodies, what importers need
	TypesInfo *types.Info    // nil when it came from the store

	// With a store: Key is the package's row ("" if none, e.g. errors), and
	// Share the graph share saved there. A package with a Share isn't walked.
	Key   string
	Share []byte
}

// mode is GoPackages' load mode. Dependencies' types come from compiled
// export data, slow on a cold build cache: why Load doesn't use go/packages.
const mode = packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedModule |
	packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo

// Walk reads one repo package: its syntax and types, or its Share if it came
// from the store. repo is every repo package's path, shared: don't change it.
// Walks run concurrently.
type Walk func(pkg *Package, repo map[string]bool)

// Load loads every package under dir, like `go list ./...`. Packages with
// errors are logged and returned with Errors set. A non-nil st serves what's
// unchanged since an earlier load. A non-nil walk runs on each repo package
// once its types are ready; Load then drops its Syntax, TypesInfo and Share.
func Load(ctx context.Context, dir string, st *store.Store, log *slog.Logger, walk Walk) (*Result, error) {
	return fromGoList(ctx, dir, st, log, walk)
}

// GoPackages loads like Load through go/packages, and also returns those
// packages. They share objects with the Result's, so SSA built from them
// maps onto its graph (vta-diff).
func GoPackages(ctx context.Context, dir string, log *slog.Logger) (*Result, []*packages.Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, err
	}

	// A repo's first load also downloads its dependencies: most of the time here.
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
