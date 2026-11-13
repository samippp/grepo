// Package cli defines grepo's command-line interface, built on cobra.
//
// The CLI is grepo's front end; `grepo mcp` (planned) will serve the same
// queries to agents. Commands stay thin: they parse arguments, call into the
// pipeline packages (source, load, graph, query), and print results.
// Analysis logic does not belong here. Each subcommand lives in its own file
// and is registered in newRootCmd.
//
// root.go also sets up logging from the global flags (-v/--verbose,
// --log-format), hands the logger to every command through app, and turns a
// command's error into the exit code, as grep's.
//
// Commands:
//   - analyze (analyze.go): analyze a GitHub link or local path and output its
//     package and call graphs as JSON.
//   - callers (callers.go): every site that can call a function, grep-style.
//     Query commands share querycmd.go: loading and positions.
//   - vta-diff (vtadiff.go, hidden): compare the call graph with x/tools' VTA.
package cli

import (
	"context"
	"errors"
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
			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			} else if cmd.Annotations[quiet] != "" {
				level = slog.LevelWarn
			}
			log, err := logging.New(cmd.ErrOrStderr(), logFormat, level)
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
	root.AddCommand(newCallersCmd(a))
	root.AddCommand(newVTADiffCmd(a))
	return root
}

// Execute runs the root command. Ctrl-C cancels the context, which stops
// in-progress clones and go toolchain calls. ExitCode turns its error into
// the process's exit code.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	a := &app{}
	err := newRootCmd(a).ExecuteContext(ctx)
	if _, reported := err.(reportedError); err != nil && !reported && !errors.Is(err, errNoResults) {
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

// errNoResults is a query that ran and found nothing: exit code 1, as grep's.
var errNoResults = errors.New("no results")

// reportedError is an error the command already printed to stderr, so
// Execute doesn't log it again.
type reportedError struct{ error }

// ExitCode is the process exit code for an error from Execute, as grep's:
// 0 for none, 1 for a query that found nothing, 2 for anything else.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errNoResults):
		return 1
	default:
		return 2
	}
}
