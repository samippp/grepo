# grepo
Grep + Repo: grep for what Go code does.

Find out how data flows through a Go repository: who calls what, what runs behind an interface, and who changes which state. grep finds text; grepo finds callers, implementations and writers, exactly.

## Who it's for

- **Contributors and people learning a codebase:** where to start reading, how the packages fit together, how a request gets from a handler to the database. Point it at a GitHub link or a fresh clone.
- **Engineers working in a codebase:** exact answers to what grep only approximates: every caller, including through interfaces; every writer of a field; everything an edit reaches. Private code on disk works the same.
- **Coding agents:** the same answers as JSON, so an agent asks one question instead of grepping and reading a dozen files.

## Like grep

One command, one question, answered from the terminal:

- Results go to stdout as `path:line` lines, clickable in editors and terminals and usable as a vim quickfix list; logs go to stderr.
- `--json` for scripts and agents.
- Graphs can also come out as Mermaid, which GitHub renders in issues, PRs and READMEs, or as DOT.
- Later runs reuse what an earlier run built, so repeat questions on a large repo don't start over.

Planned commands (M3):

```sh
grepo callers Store.Get        # who calls it, interface calls included
grepo state Store.users        # who writes it, and how
grepo impact Store.users       # everything that reaches it, by package
grepo entry                    # where requests and commands start
grepo pkg --format mermaid     # the package graph, as a diagram for an issue or PR
```

## Status

**M1: call graph.** `grepo analyze` takes a GitHub link or local path and outputs JSON: the package import graph, plus types, variables, functions, calls (with `Through`), interface implementations, and writes, reads and escapes of package vars and struct fields.

**M2a (in progress): incremental store.** Later runs reuse each unchanged package's share of the graph from a SQLite store in your user cache directory; `--no-cache` skips it.

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
| `--log-format json` | JSON log lines, for log tools (default `text`) |

Logs always go to stderr and results to stdout, so `grepo analyze <link> > graph.json` stays clean. See `go doc ./internal/logging` for what belongs at each level.

Requirements:

- **git on PATH**, for GitHub links. Only public repos are supported so far. Clones are cached under your user cache directory, and later runs update them instead of cloning again.
- **The target repo must load with the go command**, so its dependencies have to be downloadable. The first run on a repo downloads them, which can take a minute.

## Pipeline

```
GitHub link ──► source ──► load ──► graph ──► store ──► query* ──► cli commands*: path:line text, --json, --format mermaid|dot
 or local path                                                └──► grepo mcp*: the same queries as MCP tools, for agents
```

`*` = planned.

## Layout

```
cmd/grepo/         CLI entry point
internal/cli/      cobra commands
internal/logging/  slog setup
internal/source/   GitHub link or local path → directory on disk
internal/load/     go list + go/types: packages, syntax and types
internal/graph/    builds graphs from loaded packages
internal/store/    SQLite store of packages' graph shares, in the user cache
internal/vtadiff/  dev tool: compares the call graph with x/tools' VTA
```

## Roadmap

Milestone numbers are kept as they were, so references in issues and the docs stay valid.

- **M0:** GitHub link input, package import graph ✅
- **M1: the graph**
  - **M1a:** symbols, signatures and direct calls (hand-written AST walker) ✅
  - **M1b:** interface calls resolved to their implementations ✅
  - **M1c:** check the call graph against x/tools' VTA (`grepo vta-diff`) ✅
  - **M1d:** writes to package vars and struct fields ✅
  - **M1e:** reads and escapes of package vars and struct fields, plus `Through` on calls, for `impact`, dead-field detection and safer mutability claims ✅
- **M2: store + query layer** (modeled on gopls' cache)
  - **M2a: incremental store**
    - own loader: `go list` + `go/types`, dependencies type-checked from source without bodies ✅
    - SQLite store, one row per package, keyed by a hash of its files, its dependencies' keys, the build config and grepo's binary ✅
    - implementations from per-package method sets and interfaces, matched across packages afterwards ✅
    - edits rebuild the changed package and its reverse imports; everything else is reused ✅
    - repo packages walked as soon as they're checked, their syntax dropped after (#35) ✅
    - rescoped (#44): the store keeps each package's graph share, the part that costs, and types come from source. Caching deep export data cost more than it saved: on Kubernetes, filling the store took 21.1s against 8.6s without it, and a warm run saved ~1.3s
      - a repo package from the store is type-checked without bodies and its share decoded; no export data ✅
      - dependencies' export data only where it measurably beats checking them without bodies
      - warm runs: rows marked used only when stale, shares decoded in parallel; pruning by last use
      - targets: a warm run clearly faster than `--no-cache`, filling the store within ~20% of it, a run after an edit never slower
  - **M2b: query layer**
    - short names (`Store.users`, `store.Get`) resolved to IDs, with ambiguity handling
    - defaults to the package in the current directory
    - loads only what a command needs (downstream: the package and its imports; upstream: the module)
    - a query on an unchanged repo doesn't run `go list` again (file hashes cached by path, mtime and size)
  - **M2c: cold start**, the first run on a fresh clone, which is a contributor's first impression
    - metadata-only first load (`go list`, no type-checking): package graph right away
    - background indexing with progress; commands use whatever is ready
    - partial pass without dependencies: answers in seconds, marked partial until the full load lands
    - `--no-cgo`
    - import a graph built elsewhere (e.g. by CI), then rebuild only what changed locally
- **M3: CLI commands** (grep-style `path:line` output, `--json` on every command, `--format mermaid|dot` on graphs)
  - Working in code:
    - `state <type|var>`: who writes each field or var, by kind, with a role (config, live state, counter, no writes found)
    - `callers` / `callees <func>`: call trees that expand interface calls into their implementations
    - `impact <symbol>`: everything that reaches it through calls and writes, rolled up by package
  - Finding your way:
    - `pkg [path]`: imports and imported-by, exported API ranked by callers, owned state and its writers, outside calls; with no path, the repo's packages ranked by how central they are
    - `entry`: main, init, HTTP handlers, CLI commands, exported API: where to start reading (was M4)
    - `find <word>`: symbols matching a word, ranked, for when you don't know the names yet
  - For agents: `grepo mcp`, the same queries as MCP tools over stdio; one long-running process keeps the graph loaded between questions
- **M4: deeper facts**
  - side effects (`effects <func>`: reaches I/O, network, database, exec, globals)
- **M7:** end-to-end flows (`path <from> <to>`: how a request gets from a handler to the database), channel send/receive flows
- **M8:** scale: `grepo pull` for prebuilt graphs of large public repos, so a learner skips the cold start; a gopls-style daemon only if the on-disk store and `grepo mcp` aren't fast enough
- **Maybe later:**
  - **M5:** web app on the same query layer: paste a link and browse, for people who won't install Go or clone. A static HTML export of a package or a flow may cover most of it. Hosting would need sandboxed workers, rate limits and shared module caches.
- **Dropped for now:**
  - **M6:** LLM summaries. Agents can summarize from grepo's answers themselves, and the source is right there.
