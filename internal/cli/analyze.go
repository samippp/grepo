// analyze.go implements `grepo analyze`: the module's package and call graphs
// as JSON (module.go loads them). The store caches dependencies' types and
// repo packages' graph shares; --no-cache skips it.

package cli

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/samippp/grepo/internal/graph"
	"github.com/samippp/grepo/internal/logging"
	"github.com/samippp/grepo/internal/source"
)

func newAnalyzeCmd(a *app) *cobra.Command {
	var out, loader string
	var noCache bool

	cmd := &cobra.Command{
		Use:   "analyze [github-link | path]",
		Short: "Analyze a Go repository and output its package and call graphs as JSON",
		Long: `Analyze outputs a Go module's package and call graphs as JSON. A path
inside a module analyzes the whole module, with positions relative to its root.`,
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

			opts := loadOptions{goPackages: loader == "packages"} // go/packages, for comparing loaders
			if !noCache && !opts.goPackages {
				if opts.store = openStore(a.log); opts.store != nil {
					defer opts.store.Close()
				}
			}
			m, err := loadModule(ctx, a.log, arg, opts)
			if err != nil {
				return err
			}
			if opts.store != nil && len(m.shares) > 0 {
				// Walked packages' shares, so the next run needn't walk them.
				t := time.Now()
				if err := opts.store.SaveShares(ctx, m.shares); err != nil {
					m.log.Warn("saving graph shares to the store", "err", err)
				} else {
					m.log.Debug("saved graph shares", "packages", len(m.shares), "took", logging.Since(t))
				}
			}

			// Local paths have no source info: leave it out.
			var src *source.Repo
			if m.repo.URL != "" {
				src = m.repo
			}
			result := struct {
				Source *source.Repo `json:"source,omitempty"`
				*graph.PackageGraph
				*graph.CallGraph
			}{src, m.pg, m.cg}

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

			m.log.Info("analysis complete", "module", m.pg.Module, "took", logging.Since(start))
			return nil
		},
	}

	cmd.Flags().StringVarP(&out, "out", "o", "", "write JSON to this file instead of stdout")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "type-check everything from source, without reading or writing the store")
	cmd.Flags().StringVar(&loader, "loader", "", `dev: "packages" loads through go/packages instead`)
	_ = cmd.Flags().MarkHidden("loader")
	return cmd
}
