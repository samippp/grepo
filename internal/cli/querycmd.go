// querycmd.go holds what the query commands (callers, and later callees, state,
// impact, find) share: loading a repo's graph, picking the package a name
// prefers, and printing positions grep-style. Exit codes are root.go's.
//
// For now each query loads the repo from source, without the store: the store
// will keep what queries read once the commands settle it (docs/architecture.md,
// Queries).

package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samippp/grepo/internal/graph"
	"github.com/samippp/grepo/internal/load"
	"github.com/samippp/grepo/internal/logging"
	"github.com/samippp/grepo/internal/query"
	"github.com/samippp/grepo/internal/source"
)

// quiet is the annotation a query command sets: its Info logs are left out
// unless -v, like grep, which prints nothing but its results.
const quiet = "quiet"

// repoGraph is a repo loaded for queries.
type repoGraph struct {
	ix     *query.Index
	root   string // absolute: the module root
	prefer string // import path of the package in the directory asked about, "" if none
	base   string // printed paths are relative to it
}

// loadRepoGraph loads the module holding arg (a directory, "." by default, or
// a GitHub link) and builds its graph. A directory inside a module loads the
// whole module, since callers can be anywhere in it, and the package in that
// directory is the one names prefer.
func loadRepoGraph(ctx context.Context, log *slog.Logger, arg string) (*repoGraph, error) {
	start := time.Now()
	repo, err := source.Resolve(ctx, arg, log)
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(repo.Dir)
	if err != nil {
		return nil, err
	}
	root := moduleRoot(dir)

	cb := graph.NewCallGraphBuilder(root)
	res, err := load.Load(ctx, root, nil, log, cb.Add)
	if err != nil {
		return nil, err
	}
	pg := graph.BuildPackageGraph(res)
	cg, _, err := cb.CallGraph()
	if err != nil {
		return nil, err
	}
	log.Info("built graph", "functions", len(cg.Functions), "calls", len(cg.Calls), "took", logging.Since(start))

	rg := &repoGraph{ix: query.New(pg, cg), root: root, base: root}
	for _, p := range res.Packages {
		if p.Dir == dir {
			rg.prefer = p.PkgPath
		}
	}
	// A local repo's paths are printed from the current directory, so they're
	// clickable where the command ran. A clone's location is grepo's business:
	// its paths are the repo's own.
	if repo.URL == "" {
		if wd, err := os.Getwd(); err == nil {
			rg.base = wd
		}
	}
	return rg, nil
}

// moduleRoot is the nearest directory holding go.mod, from dir up; dir
// itself if there's none, and loading reports why.
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

// printer prints positions and the source lines at them.
type printer struct {
	rg    *repoGraph
	files map[string][]string // lines, by repo-relative file
}

func newPrinter(rg *repoGraph) *printer {
	return &printer{rg: rg, files: map[string][]string{}}
}

// path is a "file:line" position from the graph (relative to the repo root)
// as printed: relative to rg.base.
func (p *printer) path(pos string) string {
	file, line, _ := cutLast(pos, ":")
	abs := filepath.Join(p.rg.root, filepath.FromSlash(file))
	if rel, err := filepath.Rel(p.rg.base, abs); err == nil {
		abs = rel
	}
	if line == "" {
		return abs
	}
	return abs + ":" + line
}

// line is the source line at pos, without its indentation; "" if the file
// can't be read.
func (p *printer) line(pos string) string {
	file, line, _ := cutLast(pos, ":")
	lines, ok := p.files[file]
	if !ok {
		data, err := os.ReadFile(filepath.Join(p.rg.root, filepath.FromSlash(file)))
		if err == nil {
			lines = strings.Split(string(data), "\n")
		}
		p.files[file] = lines
	}
	var n int
	if _, err := fmt.Sscan(line, &n); err != nil || n < 1 || n > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[n-1])
}

// ambiguous prints a name's candidates, one per line as path:line (none for
// one outside the repo) and a name that picks it: the short one, or the ID
// where two short ones are the same (packages with the same name). It
// returns the error to exit with.
func (p *printer) ambiguous(w io.Writer, err *query.AmbiguousError) error {
	short := map[string]int{}
	for _, f := range err.Candidates {
		short[p.rg.ix.Short(f.ID)]++
	}
	fmt.Fprintf(w, "%s matches %d functions; name one of them:\n", err.Name, len(err.Candidates))
	for _, f := range err.Candidates {
		name := p.rg.ix.Short(f.ID)
		if short[name] > 1 {
			name = f.ID
		}
		if f.Pos == "" {
			fmt.Fprintf(w, "%s (outside the repo)\n", name)
		} else {
			fmt.Fprintf(w, "%s: %s\n", p.path(f.Pos), name)
		}
	}
	return reportedError{err}
}

// cutLast splits s around the last sep.
func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}
