// module.go loads what every command is about, the same way: the Go module
// holding the directory or GitHub link given, whole, since a function's
// callers can be anywhere in it. source.Resolve → load.Load, walking each
// repo package once it's checked → the package and call graphs.

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/samippp/grepo/internal/graph"
	"github.com/samippp/grepo/internal/load"
	"github.com/samippp/grepo/internal/logging"
	"github.com/samippp/grepo/internal/source"
	"github.com/samippp/grepo/internal/store"
)

// module is a Go module, loaded and built.
type module struct {
	repo   *source.Repo
	dir    string              // the directory asked about, absolute: the module root or inside it
	res    *load.Result        // Dir is the module root
	pkgs   []*packages.Package // with goPackages only
	pg     *graph.PackageGraph
	cg     *graph.CallGraph
	shares map[string][]byte // walked packages' shares, for the store
	log    *slog.Logger      // tagged with the repo when it's a clone
}

type loadOptions struct {
	store *store.Store // nil: everything from source
	// goPackages loads through go/packages, without the store: vta-diff's SSA
	// needs its packages, and the graph must share their objects.
	goPackages bool
}

// loadModule loads the module holding arg, a directory or a GitHub link, and
// builds its graphs. Package errors are only warnings, so partly broken repos
// still give graphs.
func loadModule(ctx context.Context, log *slog.Logger, arg string, opts loadOptions) (*module, error) {
	repo, err := source.Resolve(ctx, arg, log)
	if err != nil {
		return nil, err
	}
	if repo.URL != "" {
		log = log.With("repo", repo.URL)
	}
	dir, err := filepath.Abs(repo.Dir)
	if err != nil {
		return nil, err
	}
	m := &module{repo: repo, dir: dir, log: log}
	root := moduleRoot(dir)

	var cb *graph.CallGraphBuilder // nil: the call graph is built after the load
	if opts.goPackages {
		m.res, m.pkgs, err = load.GoPackages(ctx, root, log)
	} else {
		// Walk each package once its types are ready, so its syntax can be dropped.
		cb = graph.NewCallGraphBuilder(root)
		m.res, err = load.Load(ctx, root, opts.store, log, cb.Add)
	}
	if err != nil {
		return nil, err
	}

	t := time.Now()
	m.pg = graph.BuildPackageGraph(m.res)
	log.Info("built package graph", "packages", len(m.pg.Packages), "edges", len(m.pg.Edges), "took", logging.Since(t))

	t = time.Now()
	if cb != nil {
		m.cg, m.shares, err = cb.CallGraph() // walked during the load: this only merges
	} else {
		m.cg, m.shares, err = graph.BuildCallGraph(m.res)
	}
	if err != nil && opts.store != nil {
		return nil, fmt.Errorf("%w (--no-cache skips the store)", err)
	} else if err != nil {
		return nil, err
	}
	kinds := m.cg.CountByKind()
	log.Info("built call graph",
		"functions", len(m.cg.Functions), "calls", len(m.cg.Calls),
		"static", kinds[graph.StaticCall], "interface", kinds[graph.InterfaceCall], "dynamic", kinds[graph.DynamicCall],
		"resolved", resolvedPercent(kinds[graph.StaticCall], len(m.cg.Calls)),
		"took", logging.Since(t))
	return m, nil
}

// moduleRoot is the nearest directory at or above dir holding go.mod, or
// dir if none does (loading then reports the error).
func moduleRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

// openStore opens the shared store, or returns nil (logged) if it can't: a
// run without the cache is slower, not wrong.
func openStore(log *slog.Logger) *store.Store {
	path, err := store.DefaultPath()
	if err == nil {
		var st *store.Store
		if st, err = store.Open(path); err == nil {
			log.Debug("opened store", "path", path)
			return st
		}
	}
	log.Warn("not using the store", "err", err)
	return nil
}

// resolvedPercent formats the share of calls with a known target, e.g. "71.4%".
func resolvedPercent(static, total int) string {
	if total == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(static)/float64(total))
}
