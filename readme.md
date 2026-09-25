# grepo

Paste a GitHub link to a Go repository and get a browsable wiki showing how data flows between its packages.

grepo has two front ends that share one analysis pipeline:

- **Web app (the main product, M3):** paste a GitHub link, get the wiki.
- **CLI:** analyze links or local checkouts, including private code you already have on disk.

## Status

**M0: package graph.** `grepo analyze` takes a GitHub link or local path and outputs the package import graph as JSON.

## Usage

```sh
go build -o grepo.exe ./cmd/grepo
./grepo.exe analyze https://github.com/spf13/cobra     # GitHub link
./grepo.exe analyze ./path/to/repo -o graph.json       # local path, JSON to a file
```

Logging flags (available on every command):

| Flag | Effect |
|---|---|
| `-v`, `--verbose` | show debug logs (git commands, cache directories) |
| `--log-format json` | JSON log lines, for the server and log tools (default `text`) |

Logs always go to stderr and results to stdout, so `grepo analyze <link> > graph.json` stays clean. See `go doc ./internal/logging` for what belongs at each level.

Requirements:

- **git on PATH**, for GitHub links. Only public repos are supported so far. Clones are cached under your user cache directory, and later runs update them instead of cloning again.
- **The target repo must load with the go command**, so its dependencies have to be downloadable. The first run on a repo downloads them, which can take a minute.

## Pipeline

```
GitHub link ──► source ──► load ──► graph ──► store* ──► llm*
                  ▲                                        │
      cli (now), server* (web app)        server* ──► browser wiki
```

`*` = planned.

## Layout

```
cmd/grepo/         CLI entry point
internal/cli/      cobra commands
internal/logging/  slog setup shared by the CLI and (later) the server
internal/source/   GitHub link or local path → directory on disk
internal/load/     loads packages via golang.org/x/tools/go/packages
internal/graph/    builds graphs from loaded packages
```

## Roadmap

- **M0:** GitHub link input, package import graph ✅
- **M1:** types, SSA, VTA call graph, SQLite storage keyed by repo + commit
- **M2:** side-effect and entry-point detection
- **M3:** web app: paste a link, background analysis job, wiki pages
- **M4:** LLM summaries
- **M5:** end-to-end flows
- **M6:** scale, and hosted deployment (sandboxed workers, rate limits)
