// Package load is grepo's only point of contact with the Go toolchain.
//
// It wraps golang.org/x/tools/go/packages, which runs `go list` inside the
// target repo, so grepo sees the code exactly as `go build` would: the same
// go.mod, dependency versions and build tags. Every later stage (graph now,
// then the call graph and data-flow analysis) consumes the Result from here.
//
// What the target repo needs: it must be a Go module whose dependencies can
// be downloaded. Limitations: only files for the current GOOS/GOARCH are
// loaded, test files are skipped, and nested modules are not followed.
//
// Planned: M1 adds NeedSyntax, NeedTypes and NeedTypesInfo to the load mode so
// that graph can build SSA and the VTA call graph.
package load

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"golang.org/x/tools/go/packages"

	"grepo/internal/logging"
)

// Result is a loaded repository.
type Result struct {
	Dir      string // absolute path to the repository root
	Module   string // main module path, empty if none was found
	Packages []*packages.Package
}

// M0 only needs names, files and imports. Syntax and types get added in M1.
const mode = packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedModule

// Load loads every package under dir (the equivalent of `go list ./...`).
// Packages that fail to load are logged as warnings and still returned, with
// their Errors field set.
func Load(ctx context.Context, dir string, log *slog.Logger) (*Result, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	// The first load of a repo also downloads its dependencies, which is
	// usually most of the time spent here.
	start := time.Now()
	log.Debug("loading packages", "dir", abs)
	cfg := &packages.Config{Context: ctx, Mode: mode, Dir: abs}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading packages in %s: %w", abs, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go packages found in %s", abs)
	}

	res := &Result{Dir: abs, Packages: pkgs}
	for _, p := range pkgs {
		if res.Module == "" && p.Module != nil && p.Module.Main {
			res.Module = p.Module.Path
		}
		for _, e := range p.Errors {
			log.Warn("package has errors", "package", p.PkgPath, "err", e.Error())
		}
	}
	log.Info("loaded packages", "module", res.Module, "count", len(pkgs), "took", logging.Since(start))
	return res, nil
}
