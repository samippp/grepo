// Command grepo answers questions about how data flows through a Go
// repository, like grep for what the code does: who calls what, what runs
// behind an interface, and who changes which state.
//
// This file is only the entry point: it hands off to internal/cli and exits
// with the code cli.ExitCode gives, as grep's: 0, 1 when a query found
// nothing, 2 on an error. All behavior lives under internal/.
//
// Pipeline (packages marked * are planned, see readme.md for milestones):
//
//	GitHub link ──► source ──► load ──► graph ──► store ──► query ──► cli commands
//	 or local path                                        └──► grepo mcp* (agents)
package main

import (
	"os"

	"github.com/samippp/grepo/internal/cli"
)

func main() {
	os.Exit(cli.ExitCode(cli.Execute()))
}
