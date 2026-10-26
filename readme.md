# grepo
Grep + Repo: 
Find out how data flows through a Go repository: who calls what, what runs behind an interface, and who changes which state.

grepo has two front ends that share one analysis pipeline and query layer:

- **CLI (first, M3):** answer questions about the repo you're in from the terminal, including private code you already have on disk. For engineers who'd rather go straight to the code than read a wiki.
- **Web app (M5):** paste a GitHub link, get a browsable wiki with diagrams.

## Status

**M1: call graph.** `grepo analyze` takes a GitHub link or local path and outputs JSON: the package import graph, plus types, variables, functions, calls (with `Through`), interface implementations, and writes, reads and escapes of package vars and struct fields.

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
GitHub link ──► source ──► load ──► graph ──► store* ──► query* ──► cli commands*
 or local path                                                 └──► server* ──► browser wiki (+ llm*)
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
internal/vtadiff/  dev tool: compares the call graph with x/tools' VTA
```

## Roadmap

- **M0:** GitHub link input, package import graph ✅
- **M1: the graph**
  - **M1a:** symbols, signatures and direct calls (hand-written AST walker) ✅
  - **M1b:** interface calls resolved to their implementations ✅
  - **M1c:** check the call graph against x/tools' VTA (`grepo vta-diff`) ✅
  - **M1d:** writes to package vars and struct fields ✅
  - **M1e:** reads and escapes of package vars and struct fields, plus `Through` on calls, for `impact`, dead-field detection and safer mutability claims ✅
- **M2: store + query layer** (modeled on gopls' cache)
  - **M2a: incremental store**
    - metadata-only first load (`go list`, no type-checking): package graph right away
    - SQLite store, one entry per package, keyed by a hash of its files, its dependencies' keys and the build config
    - edits rebuild the changed package and its reverse imports; everything else is reused
    - implementations from per-package method sets and interfaces, matched across packages afterwards
    - own loader: `go list` + `go/types`, dependencies type-checked from source without bodies, type summaries cached per package
  - **M2b: query layer**
    - short names (`Store.users`, `store.Get`) resolved to IDs, with ambiguity handling
    - defaults to the package in the current directory
    - loads only what a command needs (downstream: the package and its imports; upstream: the module)
  - **M2c: cold start**
    - background indexing with progress; commands use whatever is ready
    - partial pass without dependencies: answers in seconds, marked partial until the full load lands
    - `--no-cgo`
    - import a graph built elsewhere (e.g. by CI), then rebuild only what changed locally
- **M3: CLI commands** (human output with editor-clickable `file:line`, plus `--json`)
  - `state <type|var>`: who writes each field or var, by kind, with a role (config, live state, counter, no writes found)
  - `callers` / `callees <func>`: call trees that expand interface calls into their implementations
  - `pkg [path]`: imports and imported-by, exported API ranked by callers, owned state and its writers, outside calls
  - `impact <symbol>`: everything that reaches it through calls and writes, rolled up by package
- **M4: deeper facts**
  - side effects (`effects <func>`: reaches I/O, network, database, exec, globals)
  - entry points (`entry`: main, init, HTTP handlers, CLI commands, exported API)
- **M5:** web app: paste a link, background analysis job, wiki pages built on the same queries
- **M6:** LLM summaries
- **M7:** end-to-end flows (`path <from> <to>`), channel send/receive flows
- **M8:** scale, and hosted deployment (sandboxed workers, rate limits, shared module caches, `grepo pull` for prebuilt public graphs); a gopls-style daemon only if the on-disk cache isn't fast enough
