// analyze.go implements `grepo analyze [github-link | path] [-o file]`.
//
// Flow: source.Resolve (clone the GitHub repo, or use the local path) →
// load.Load (run the go toolchain) → graph.BuildPackageGraph (reshape the
// result) → JSON to stdout or the --out file. Each stage logs what it did and
// how long it took. Errors in individual packages are logged as warnings rather
// than failing the run, so partly broken repos still produce output.
//
// Planned: from M1 this also writes the results to a SQLite database, which
// `grepo serve` reads.

package cli

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"grepo/internal/graph"
	"grepo/internal/load"
	"grepo/internal/logging"
	"grepo/internal/source"
)

func newAnalyzeCmd(a *app) *cobra.Command {
	var out string

	cmd := &cobra.Command{
		Use:   "analyze [github-link | path]",
		Short: "Analyze a Go repository and output its package graph as JSON",
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

			res, err := load.Load(ctx, repo.Dir, log)
			if err != nil {
				return err
			}

			t := time.Now()
			g := graph.BuildPackageGraph(res)
			log.Info("built package graph",
				"packages", len(g.Packages), "edges", len(g.Edges), "took", logging.Since(t))

			// Local paths have no source info, so leave the field out entirely.
			var src *source.Repo
			if repo.URL != "" {
				src = repo
			}
			result := struct {
				Source *source.Repo `json:"source,omitempty"`
				*graph.PackageGraph
			}{src, g}

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
	return cmd
}
