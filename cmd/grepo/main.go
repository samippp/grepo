// Command grepo builds a browsable wiki of how data flows through a Go
// repository.
//
// This file is only the entry point: it hands off to internal/cli and turns an
// error into a non-zero exit code. All behavior lives under internal/.
//
// Pipeline (packages marked * are planned, see readme.md for milestones):
//
//	GitHub link ──► source ──► load ──► graph ──► store* ──► llm*
//	                  ▲                                        │
//	      cli (now), server* (web app)        server* ──► browser wiki
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
