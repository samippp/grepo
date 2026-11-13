// Package cli is grepo's command-line interface, built on cobra. Commands
// stay thin: they parse arguments, call the pipeline packages (source, load,
// graph, query) and print results. Each has its own file:
//   - analyze: the package and call graphs as JSON.
//   - callers: every site that can call a function, grep-style.
//   - vta-diff (hidden): compare the call graph with x/tools' VTA.
//
// module.go loads the module every command is about; query commands share
// querycmd.go; root.go sets up logging and exit codes.
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

// app is state shared by all commands. log is set in PersistentPreRunE,
// once flags are parsed.
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

// Execute runs the root command. Ctrl-C cancels its context, stopping
// clones and go toolchain calls.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	a := &app{}
	err := newRootCmd(a).ExecuteContext(ctx)
	if _, reported := err.(reportedError); err != nil && !reported && !errors.Is(err, errNoResults) {
		// Errors from before the logger exists (bad flags) are printed plainly.
		if a.log != nil {
			a.log.Error("command failed", "err", err)
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
	}
	return err
}

// errNoResults is a query that found nothing: exit code 1.
var errNoResults = errors.New("no results")

// reportedError is an error already printed, which Execute doesn't log.
type reportedError struct{ error }

// ExitCode is grep's exit code for Execute's error: 0 for none, 1 for no
// results, 2 for anything else.
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
