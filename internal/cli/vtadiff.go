// vtadiff.go implements the hidden dev command `grepo vta-diff`, which
// compares grepo's call graph with x/tools' VTA and prints every difference.

package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/samippp/grepo/internal/logging"
	"github.com/samippp/grepo/internal/vtadiff"
)

func newVTADiffCmd(a *app) *cobra.Command {
	var examples int

	cmd := &cobra.Command{
		Use:    "vta-diff [github-link | path]",
		Short:  "Dev tool: compare grepo's call graph with x/tools' VTA",
		Hidden: true,
		Args:   cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := "."
			if len(args) == 1 {
				arg = args[0]
			}
			m, err := loadModule(cmd.Context(), a.log, arg, loadOptions{goPackages: true})
			if err != nil {
				return err
			}

			t := time.Now()
			report := vtadiff.Compare(m.res, m.pkgs, m.cg)
			m.log.Info("compared with VTA", "took", logging.Since(t))
			return report.Write(cmd.OutOrStdout(), examples)
		},
	}

	cmd.Flags().IntVarP(&examples, "examples", "n", 15, "examples to show per category (0 = all)")
	return cmd
}
