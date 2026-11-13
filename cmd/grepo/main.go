// Command grepo answers questions about how data flows through a Go repo:
// who calls what, what runs behind an interface, who changes which state.
// main runs internal/cli and exits with cli.ExitCode; the pipeline is:
//
//	GitHub link ──► source ──► load ──► graph ──► store ──► query ──► cli
//	 or local path
package main

import (
	"os"

	"github.com/samippp/grepo/internal/cli"
)

func main() {
	os.Exit(cli.ExitCode(cli.Execute()))
}
