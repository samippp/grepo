// analyze.go implements `grepo analyze [github-link | path] [-o file]`.
//
// Flow: source.Resolve (clone the GitHub repo, or use the local path) →
// load.Load (run the go toolchain, walking each repo package for the call
// graph as soon as it's checked) → graph.BuildPackageGraph, and merging the
// call graph → JSON to stdout or the --out file. Each stage logs what it did and
// how long it took. Errors in individual packages are logged as warnings rather
// than failing the run, so partly broken repos still produce output.
//
// Dependencies' types are cached in the shared store (internal/store), so a
// later run doesn't type-check them again; --no-cache loads everything from
// source. Planned: the graph is stored too, for the query layer and `grepo serve`.

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/samippp/grepo/internal/graph"
	"github.com/samippp/grepo/internal/load"
	"github.com/samippp/grepo/internal/logging"
	"github.com/samippp/grepo/internal/source"
	"github.com/samippp/grepo/internal/store"
)

func newAnalyzeCmd(a *app) *cobra.Command {
	var out, loader string
	var noCache bool

	cmd := &cobra.Command{
		Use:   "analyze [github-link | path]",
		Short: "Analyze a Go repository and output its package and call graphs as JSON",
		Example: `  grepo analyze https://github.com/spf13/cobra
  grepo analyze ./path/to/repo -o graph.json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			arg := "."
			if len(args) == 1 {
				arg = args[0]
			}
			ctx := cmd.Context()
			log := a.log

			repo, err := source.Resolve(ctx, arg, log)
			if err != nil {
				return err
			}
			// Tag every later log line with the repo being analyzed.
			if repo.URL != "" {
				log = log.With("repo", repo.URL)
			}

			var res *load.Result
			var st *store.Store            // nil: no store, or --loader packages
			var cb *graph.CallGraphBuilder // nil: the call graph is built after the load
			if loader == "packages" {      // the go/packages path, for comparing loaders
				res, _, err = load.GoPackages(ctx, repo.Dir, log)
			} else {
				// Each repo package is walked as soon as its types are ready,
				// so its syntax can be dropped instead of held until the load ends.
				var dir string
				if dir, err = filepath.Abs(repo.Dir); err != nil { // what Load reports as Result.Dir
					return err
				}
				cb = graph.NewCallGraphBuilder(dir)
				if !noCache {
					st = openStore(log)
					if st != nil {
						defer st.Close()
					}
				}
				res, err = load.Load(ctx, dir, st, log, cb.Add)
			}
			if err != nil {
				return err
			}

			t := time.Now()
			g := graph.BuildPackageGraph(res)
			log.Info("built package graph",
				"packages", len(g.Packages), "edges", len(g.Edges), "took", logging.Since(t))

			t = time.Now()
			var cg *graph.CallGraph
			var shares map[string][]byte
			if cb != nil {
				cg, shares, err = cb.CallGraph() // packages were walked during the load: this merges them
			} else {
				cg, shares, err = graph.BuildCallGraph(res)
			}
			if err != nil {
				return fmt.Errorf("%w (--no-cache skips the store)", err)
			}
			kinds := cg.CountByKind()
			log.Info("built call graph",
				"functions", len(cg.Functions), "calls", len(cg.Calls),
				"static", kinds[graph.StaticCall], "interface", kinds[graph.InterfaceCall], "dynamic", kinds[graph.DynamicCall],
				"resolved", resolvedPercent(kinds[graph.StaticCall], len(cg.Calls)),
				"took", logging.Since(t))
			if st != nil && len(shares) > 0 {
				// Walked packages' shares, so the next run needn't walk them.
				t = time.Now()
				if err := st.SaveShares(ctx, shares); err != nil {
					log.Warn("saving graph shares to the store", "err", err)
				} else {
					log.Debug("saved graph shares", "packages", len(shares), "took", logging.Since(t))
				}
			}

			// Local paths have no source info, so leave the field out entirely.
			var src *source.Repo
			if repo.URL != "" {
				src = repo
			}
			result := struct {
				Source *source.Repo `json:"source,omitempty"`
				*graph.PackageGraph
				*graph.CallGraph
			}{src, g, cg}

			var w io.Writer = cmd.OutOrStdout()
			if out != "" {
				f, err := os.Create(out)
				if err != nil {
					return err
				}
				defer f.Close()
				w = f
			}
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			if err := enc.Encode(result); err != nil {
				return err
			}

			log.Info("analysis complete", "module", g.Module, "took", logging.Since(start))
			return nil
		},
	}

	cmd.Flags().StringVarP(&out, "out", "o", "", "write JSON to this file instead of stdout")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "type-check everything from source, without reading or writing the store")
	cmd.Flags().StringVar(&loader, "loader", "", `dev: "packages" loads through go/packages instead`)
	_ = cmd.Flags().MarkHidden("loader")
	return cmd
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
