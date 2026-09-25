// Package cli defines grepo's command-line interface, built on cobra.
//
// The CLI is one of two front ends. The other is the web app (M3), where users
// paste a GitHub link. Both drive the same pipeline, so commands stay thin:
// they parse arguments, call into the pipeline packages (source, load, graph,
// and later store/llm), and print results. Analysis logic does not belong here.
// Each subcommand lives in its own file and is registered in newRootCmd.
//
// root.go also sets up logging from the global flags (-v/--verbose,
// --log-format) and hands the logger to every command through app.
//
// Commands:
//   - analyze (analyze.go): analyze a GitHub link or local path and output its
//     package graph as JSON.
//   - serve (planned, M3): host the web UI for an analyzed repo.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/samippp/grepo/internal/logging"
)

// app holds state shared by all commands. The logger is set in
// PersistentPreRunE, after flags are parsed and before any command runs.
type app struct {
	log *slog.Logger
}

func newRootCmd(a *app) *cobra.Command {
	var (
		verbose   bool
		logFormat string
	)

	root := &cobra.Command{
		Use:           "grepo",
		Short:         "Map how data flows through a Go codebase",
		SilenceUsage:  true,
		SilenceErrors: true, // Execute logs errors itself
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			log, err := logging.New(cmd.ErrOrStderr(), logFormat, verbose)
			if err != nil {
				return err
			}
			a.log = log
			return nil
		},
	}
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "show debug logs")
	root.PersistentFlags().StringVar(&logFormat, "log-format", "text", "log format: text or json")

	root.AddCommand(newAnalyzeCmd(a))
	return root
}

// Execute runs the root command. Ctrl-C cancels the context, which stops
// in-progress clones and go toolchain calls.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	a := &app{}
	err := newRootCmd(a).ExecuteContext(ctx)
	if err != nil {
		// Errors before the logger exists (bad flags, bad --log-format) can't
		// be logged, so print them plainly.
		if a.log != nil {
			a.log.Error("command failed", "err", err)
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
	}
	return err
}
