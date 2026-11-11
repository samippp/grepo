// Command grepo answers questions about how data flows through a Go
// repository, like grep for what the code does: who calls what, what runs
// behind an interface, and who changes which state.
//
// This file is only the entry point: it hands off to internal/cli and turns an
// error into a non-zero exit code. All behavior lives under internal/.
//
// Pipeline (packages marked * are planned, see readme.md for milestones):
//
//	GitHub link ──► source ──► load ──► graph ──► store ──► query* ──► cli commands*
//	 or local path                                         └──► grepo mcp* (agents)
package main

import (
	"os"

	"github.com/samippp/grepo/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
