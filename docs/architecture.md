# grepo architecture

How grepo turns a Go repository into a graph, how the graph is shaped, and the design decisions behind it. Update this file when a decision changes.

## 1. Pipeline

```
GitHub link ──► source ──► load ──► graph ──► JSON (stdout / -o)
               clone       go/packages:      BuildPackageGraph  → packages + import edges
               + commit    syntax trees,     BuildCallGraph     → types, variables, functions,
                           types, TypesInfo                        calls, implementations
```

| Package | Role |
|---|---|
| `internal/source` | GitHub link → shallow clone in the user cache, plus the commit hash. Local paths work too. |
| `internal/load` | Loads packages through `go/packages` with syntax trees and full type information. |
| `internal/graph` | Pure transformation: no I/O, no logging, sorted deterministic output. |
| `internal/logging` | `slog` setup; logs to stderr, results to stdout. |
| `internal/cli` | `analyze`, plus the hidden dev command `vta-diff`. |
| `internal/vtadiff` | Compares grepo's call graph with x/tools' VTA and reports differences. |

## 2. The graph

Nodes are things; relationships are separate edge lists.

```
CallGraph
├── Types           []Type             nodes
├── Variables       []Variable         nodes
├── Functions       []Function         nodes (incl. methods and interface methods)
├── Calls           []FunctionCall     edges: caller → callee
├── Accesses        []Access           edges: function → var / field it writes, reads or escapes
└── Implementations []Implementation   edges: type → interface
(+ PackageGraph: package nodes and import edges)
```

### Nodes

**`Type`**: every named type in the repo, including types declared inside functions, one struct with a kind.

```
ID, Name, Package, Kind, Exported, Pos
Kind: struct | interface | alias | other
Kind-specific fields (flat; empty for the other kinds):
  Fields      struct     → []Field, each with its own ID ("pkg.Store.users") and Embedded flag
  Embeds      interface  → embedded interface type IDs (referenced, not copied)
  Target      alias      → the aliased type
  Underlying  other      → e.g. "int", "func(string)"
```

A type's methods are not stored on it; they are derived from `Function.Owner`.

**`Function`**: plain functions, methods, interface methods and closures.

```
ID, Name, Package, Receiver (*Param, nil if none), Params, Results, Exported, Pos
Owner  → ID of the type that declares it (methods only)
Parent → closures only: the enclosing function or closure, or the package var it initializes
```

Interface methods are `Function` nodes with no `Receiver`, `Owner` set to the interface, and no calls. Closures have no `Receiver` or `Owner`, even inside methods, and are never exported.

**`Variable`**: package-level `var` and `const`.

```
ID ("pkg.name"), Name, Package, Type, Exported, Pos
Const → flag, like C's const qualifier
Value → consts only, exact (constant.Value.ExactString)
```

### Edges

**`FunctionCall`**: one per call site.

```
Caller    a function ID, or a package path for package-level calls
Callee    a function ID; "" for dynamic calls
Pos       "file:line" of the call site (start of the call expression)
CallKind  static | interface | dynamic
Args      []{Expr as written, Type, Func}
          Func: the function the argument is (a closure, a declared function, a
          method value: sort.Slice(xs, func…), HandleFunc("/", h)); "" otherwise
Through   the field or package var the call is made on (s.buf.WriteString) or takes
          its func from (s.onError(err)); "" for locals, params, results
Stmt      "go" | "defer" when the call is that statement's; "" otherwise
```

**`Access`**: one per write, read or escape site.

```
Accessor  a function ID, or a package path for package-level values
Target    a VarID or FieldID, in the repo or not
Pos       "file:line" of the site (start of the expression)
Kind      assign | update | content | construct | read | escape
Func      assign / construct: the function the written value is, like Arg.Func
          (cobra.Command{RunE: func…}, s.onErr = h); "" otherwise
```

**`Implementation`**: a concrete repo type satisfies a repo interface.

```
Type, Interface   type IDs
PointerOnly       only *Type implements it
```

## 3. How it connects

```
                  Owner                       Type / Interface
   Function ─────────────────► Type ◄──────────── Implementation
      ▲  ▲                      │ Fields: "pkg.Store.users"
      │  │ Callee               │ Embeds: other Type IDs
      │  │                      │
      │  └──── FunctionCall ────┘
      │ Caller     │
      │            │ Caller (package-level calls)
      │            ▼
   (function)   Package  (PackageGraph node)
```

Every reference is an ID string, and every ID resolves to a node. Exceptions by design: a dynamic call's empty callee, and callees outside the repo (e.g. `fmt.Println`), which are kept but have no node.

Example: what can `lookup` reach through `g.Get(1)`?

1. Call edge: `lookup → (store.Getter).Get` (interface)
2. That function node: `Owner = store.Getter`
3. Implementations with `Interface = store.Getter`: `Store` (pointer only), `Cache`
4. Functions with `Owner = Store` and `Name = Get`: `(*store.Store).Get`

## 4. How it's built

```
BuildCallGraph
  for each package: buildPackage (fresh builder, nothing shared but the repo's package paths)
  → for each file → declaration:
    FuncDecl → handleFuncDecl      Function node (functions.go; ID via assignFuncDeclID, Owner via receiverOwner)
                └ walk      recursive descent (walk.go): walk = statements, expr = values,
                            children = no context change. calls → addCall (calls.go,
                              + through, stmt);
                            func literals → addClosure (functions.go): a Function node
                              (ID from numberClosures' pre-pass), then its body with its own ID;
                            local TYPE decls → assignLocalTypeID + handleTypeSpec;
                            writes (writes.go): =, op=, ++, range = → addAssign / addWrite,
                              which hand what they don't write to expr;
                            struct literals → addConstruct; delete/clear/copy → addBuiltinWrite;
                            reads and escapes (reads.go): idents / selectors → addValueRead /
                              addSelector; & → addAddress; receivers → addReceiver;
                              passed / stored / returned / sent → handed
    GenDecl  → handleGenDecl       switch on token:
                ├ TYPE      → handleTypeSpec   Type node (types.go); struct → structFields;
                │                              interface → embeds + addInterfaceMethod;
                │                              summarize: method sets by key (implementations.go);
                │                              consts in the type expr read by the package
                └ VAR/CONST → handleValueSpec  Variable nodes (variables.go); construct edges
                                               for vars with a value; calls and accesses in
                                               values with the package as caller
  after every package: merge the shares (nodes, edges, local type and closure IDs)
    matchImplementations   (implementations.go) first-method-key index → method sets compared by key
    sort every list
```

## 5. ID rules

| Thing | Format | Example |
|---|---|---|
| Function | `FullName()` | `example.com/simple.greet` |
| Method | `(receiver).name`, receiver = the declaring type's ID (`CallGraph.FuncID`) | `(*…store.Store).Get` |
| Interface method | `(interface).name` | `(…store.Getter).Get` |
| `init` | `pkg.init@file#n`, numbered per file | `…simple.init@main.go#2` |
| Type | `pkg.Name` (`CallGraph.TypeID`); generics keep param names, not constraints; built-ins have no package | `…simple.List[T]`, `error` |
| Closure | parent's ID + `$n`, numbered per parent in source order; nested closures nest | `…simple.main$1`, `…simple.main$1$2` |
| Package-level closure | the var it initializes + `$n`; blank vars use the package (`pkg._$n`) | `…store.OnReady$1` |
| Local type | enclosing function's or closure's ID + `.name#n`, numbered per name in source order | `…simple.save.result#2`, `…simple.spawn$1.job#1` |
| Field | type ID + `.` + name | `…store.Store.users` |
| Variable | `pkg.name` | `…store.defaultStore` |
| Position (`Pos`) | `file:line`, relative to the repo root: where the record's own syntax is. A node's declaration; an edge's site (the call, the write; a literal's field). The other ends' positions are on their nodes. | `store/store.go:23` |

`(X).Name` follows Go's method-expression syntax: `(Getter).Get` is valid Go. Because the receiver is the type's ID, a method's ID always matches its `Owner`, even when the receiver renames type parameters (`func (l *List[E])` → `List[T]`) or is written through an alias (`func (s *S)` with `S = Store` → `Store`).

## 6. Design decisions

### Graph shape
- Separate node types (`Type`, `Function`, `Variable`) rather than one generic `Symbol`.
- Relationships are their own edge lists (`Calls`, `Implementations`), not fields on nodes.
- Single source of truth: a type's methods are derived from `Function.Owner`, not stored on the type.
- Kind-specific data on `Type` is flat, rather than separate structs per kind or sub-structs. References to a type don't care about its kind, which is why there's one `Type`.

### IDs
- Stable string IDs from the type checker, not auto-increment numbers: edges compute callee IDs without lookups, and URLs and caches need stable IDs.
- Generic types keep parameter names (`List[T]`) but not constraints.
- `init` functions get file + position (`@file#n`); inserting an `init` above another shifts its number (accepted: nothing can call `init`, so no edges break).
- Method IDs use the type's ID as the receiver (consistent with `Owner`; normalizes renamed type parameters and constraints).
- Call positions are `file:line` at the start of the call expression.
- Local types: `parent.name#n`, always numbered. Chosen over `pkg.name@file:line` because the ID says where the type lives (useful to the MCP) and survives line shifts (wiki URLs). The parent is the innermost enclosing function or closure (`spawn$1.job#1`), or a package-level closure's ID.
- Local type IDs are assigned during the walk and kept in an `idTable` that `funcID` / `typeID` consult; the graph keeps it, so `CallGraph.FuncID` / `TypeID` work for consumers like vtadiff. A local type missing from the table gets `#?` rather than silently taking a package-level ID.

### Functions and calls
- A method is a `Function` with a receiver, not a separate struct.
- One edge per call site. Calls inside closures belong to the closure (see Closures).
- Built-ins and conversions are not calls. Calls to code outside the repo are kept (side-effect detection needs them).
- Interface calls point at the interface method. Dynamic calls have an empty callee; targets are never guessed.
- `Args` are recorded on each call (expression as written + type).
- Generic calls link to the generic declaration (`Origin()`); type arguments are deferred (#8).
- `unindex` strips any index expression, including ordinary `[0]`; harmless, since the type checker then reports a variable.

### Types and interfaces
- Kinds: struct, interface, alias, other ("defined type" means something else in Go).
- Aliases are their own kind with `Target`, and have no fields or methods of their own.
- Interface methods: explicit ones only; embedded ones belong to their own interface and are linked through `Embeds`.
- Owners are resolved through the type checker (pointer removed, alias resolved, then `typeID`), not by editing strings.

### Interface resolution (M1b)
- Record facts (`Implementation`) instead of "might call" edges, which would be noisy (caddy's `Module` has 137 implementations).
- `PointerOnly` distinguishes `*T` from `T`.
- Repo interfaces and repo concrete types only (outside interfaces: #14). Generic and empty interfaces are skipped.
- Matched after every package is built, from each package's stored method sets (see Implementations from summaries), so a cached package takes part without type-checking.
- A first-method-key index picks candidates; the method sets decide.

### Writes (Access)
- Purpose: who changes which state. Field roles for the MCP (config vs. live state vs. dead), ranking important vars and fields, data flow through shared state, side effects.
- Writes first; reads and escapes later as more kinds on the same edge (M1e), so `Access` is one list with kinds, like `Calls` with `CallKind`.
- Kinds are recorded because only the walker sees the syntax; later consumers would have to re-parse.
  - `assign`: `x = v`, `s.f = v`, one edge per left-hand item.
  - `update`: `x++`, `x += v`, or an `=` whose right side reads the same expression (`s.items = append(s.items, v)`). Same expression, not same ID: `b.N = o.N + 1` is an assign.
  - `content`: writing into what a field or var holds: `x[k] = v`, `x[k]++`, `*x = v`, `delete` / `clear` / `copy` (built-ins, so certain).
  - `construct`: a struct literal's fields (`T{f: v}`, positional by index), and a package var's initial value (by the package). Kept apart from `assign` so "set at construction, never mutated" is visible.
- Paths: writing through a struct-valued field also changes it: `s.inner.x = 1` is `assign Inner.x` + `content Store.inner`, for every struct value on the path (embedded ones too).
- The walk stops at the first slice, map or pointer, and the field holding it gets `content`: `s.ptr.x = 1` is `assign T.x` + `content Store.ptr`, like `s.items[0].x = 1` gives `content Store.items`. So `content F` means the data F gives access to changed, not F's own bytes; `(*s.ptr).x` and `s.ptr.x` agree.
- Field IDs are per type, so a field write counts whatever the root (`u.Name = x` on a local `u` is `User.Name`). Writes to locals themselves (`local = v`, `*p = v`, `local[i] = v`) get no edge.
- Outside targets are kept (`os.Args`, `net/http.Server.Addr`), like outside callees.
- One edge per site, with `Pos`; aggregate later if needed.
- Not writes: taking an address (`&x`, pointer-method calls like `s.mu.Lock()`) and passing slices or maps to functions (`slices.Sort(s.items)`). The write happens elsewhere, or never: see open items.

### Reads and escapes (M1e)
- Purpose: `impact` (who depends on a field), dead-field detection, and mutability claims that never say "unchanged" when it might be.
- Two kinds, no subkinds. `read`: the value was loaded here. `escape`: access was handed out here, so a change can happen elsewhere. "Who uses X" is `read` ∪ `escape`; writes are separate.
- One edge per site, like writes and calls: `f(s.x, s.x)` is two edges. `Pos` is the start of the expression.
- Reads: plain uses, `range`, indexing (`s.items[i]` reads `Store.items`), `len` / `cap` args, `copy`'s source.
- Every hop of a selector is a read, pointers included: `s.ptr.x` reads `T.x` and `Store.ptr`. Embedded hops count: `s.x` via `Base` reads `Base.x` and `Store.Base`.
- No read where the written path gets a write edge: `s.inner.x = 1` is writes only. Other hops on the path and index expressions are reads: `s.sub.ptr.x = 1` reads `Store.sub`; `s.m[s.key] = v` reads `Store.key`.
- An `update` suppresses the read of its matching right-side expression: `x += 1`, `x = x + 1` and `s.items = append(s.items, v)` are `update` only; other right-side reads stay.
- Escapes of a field's own storage (no read): `&s.f`; a method call where Go takes the address (pointer receiver on a non-pointer, non-interface field or var: `s.buf.WriteString(x)`, `s.mu.Lock()`); a method value in that case (`f := s.buf.WriteString`). Struct-value hops before the target escape too, up to the first pointer, like writes give them `content`: their memory holds the target, and field IDs are per type, so an escape of `A.b` alone can't say it was `Store.a`'s. They get no read. `&s.a.b` escapes `A.b` and `Store.a`; `&s.p.b` escapes `P.b` and reads `Store.p`; `s.a.inner.Lock()` escapes `A.inner` and `Store.a`.
- Escapes of contents (plus the read): a slice or map field or var passed to a function, stored (`m := s.users`, `o.x = s.items`, `T{items: s.items}`), returned, or sent. Only when the expression is the field itself (not `s.items[1:]`); builtins and conversions excluded. Pointers excluded on purpose: "the contents of `db` may change" is true of every pointer field and tells users nothing.
- Other method calls through a field are reads: pointer field (`s.db.Query`), value receiver (`s.cfg.Addr`), interface (`s.h.ServeHTTP`), func field (`s.onError(err)`).
- Consts: repo consts are read; outside consts (`time.Second`, `http.StatusOK`) get no edge: they can't change and no consumer needs them. Outside fields and vars are kept (`r.Header`, `os.Args`), like writes; human output hides them by default.
- Consts are read wherever a type is written: type declarations, signatures (closures too), var types, literal types (`[Max]byte{}`), conversions. Package-level values and types (`type T [Max]byte`, `var t [Max]int`) have the package as accessor; code inside package-level closures has the closure. Generic fields use `Origin()`, like writes.
- Don't guess what callees do: whether a method changes its receiver isn't decidable at the site (outside bodies are dropped, interfaces have many implementations), so the site records facts and a later analysis may summarize methods (needs the root of each write).

### Through (M1e)
- On `FunctionCall`, not `Access`: it links a call to the stored value it goes through, for flows (M7) and for "which methods are called on this field".
- Set when the call's `X` in `X.M()` or `X()` is a field or package var; promoted methods use the embedded field (`l.Lock()` via `sync.Mutex` → `Locked.Mutex`). Locals, params, results and package names: "".
- The mode (value, pointer, address, interface, func) isn't stored: it follows from `CallKind`, the target's type and the callee's receiver (`(*T).M`). The walker computes it to choose read or escape.

### Walker structure (M1e)
- Recursive descent instead of one `ast.Inspect`: `walk` (statements), `expr` (an expression evaluated as a value), `children` (everything that doesn't change context).
- Context is explicit: a subexpression is a read only if it reaches `expr`. Write handlers (`addWrite`) hand what they don't write (index expressions, the path past a pointer) to `expr`; `&`, receivers and an update's matching expression don't go to `expr`.
- One exception: an update's matching right-side expression goes in `suppressed`, which `expr` checks, instead of being passed down through every call. It's always a chain of idents and selectors, so skipping it drops nothing else.
- Chosen over mark-and-skip (handlers mark nodes, a later visit skips them): context stays in one place, and a forgotten case gives a missing read instead of a spurious one.
- First as a behavior-preserving refactor: existing tests and the `vta-diff` report unchanged, before any reads.

### Closures (#13)
- Purpose: calls *to* closures. Inline HTTP handlers, goroutines, `sort.Slice` comparators and cobra `RunE`s are closures; without nodes, entry points (M4) can't find inline handlers and flows (M7) can't enter them. Done before M2 because it changes existing edges and IDs, and the store and query layer should be built on the final shape.
- A closure is a `Function` with `Parent`, not a separate struct, like methods. Everything inside it (calls, accesses, local types, nested closures) belongs to it: "everything `f` does" rolls up through `Parent`. Kept apart rather than merged into the enclosing function, because a goroutine or handler body runs at a different time, often many times, and M4 / M7 need to tell them apart.
- IDs: parent + `$n` in source order, like SSA (`main$1`, `main$1$2`). Numbered in a pre-pass over each body, not during the walk, so the order is the source order even where the walk visits a statement's parts out of order (writes before right sides). Inserting a closure renumbers later ones in the same parent: accepted, like `init`; local types use `#n` per name, but closures have no name to number by.
- Package-level closures take the var they initialize as parent (`OnReady$1`), not the package: the ID says what it is, and it doesn't depend on file order. `var a, b = f1, f2` pairs values with names; one value for several names uses the first. Blank vars have no node, so their closures use the package (`pkg._$n`).
- Calls to closures:
  - Called where it's written (`func(){…}()`, `go func(){…}()`, `defer func(){…}()`): a static call from the parent to the closure. These are the only calls whose target is a closure.
  - Through a local, field or var (`inc := func(){…}; inc()`, `s.onErr(err)`): still dynamic. Targets are never guessed; the value could be reassigned.
- `Func` on `Arg` and on `assign` / `construct` accesses records the function a value *is*, where it lands, so consumers can follow it without guessing: `HandleFunc("/", func…)` links the call to the handler; `cobra.Command{RunE: func…}` links the field to it; `s.onErr = h` plus `Through = Store.onErr` on a later call lets M7 list candidates. It's set only when the expression itself is the function: a closure, a declared function or generic instance (`Origin()`), or a method value or expression. Locals holding functions, returns, and elements of slice / map literals get nothing.
- `Stmt` on calls: whether the call is `go` or `defer`. A goroutine is where concurrency starts (M4 effects, M7 flows), and the walker is the only place that sees the statement. Applies to every call, not just closures (`go s.run()`).
- Calls inside closures moving to the closure changes existing edges: `callers greet` now shows `main$1`, whose `Parent` is `main`. The human output (M3) can roll this up.
- `vta-diff` matches closures by syntax: SSA keeps each closure's `*ast.FuncLit` (`fn.Syntax()`), the node grepo walked, so no numbering has to agree. A range-over-func loop body is a closure to SSA but not in the source; it stays in the code around the loop.

### Variables
- Package level only: local `var` / `const` get no node (an LLM reads function bodies from source).
- `var` and `const` share one struct, with a `Const` flag. Consts store exact values; untyped consts keep `untyped int`.
- Blank (`_`) variables get no node, but calls in their values are still recorded.
- Package-level calls use the package as the caller: an existing node, nothing synthetic.

### Per-package building (M2a)
- `buildPackage(pkg, root, repo)` walks one package with a fresh builder; `BuildCallGraph` merges the results, matches implementations and sorts. The cache stores one package's share, so a share can't depend on any other package's walk.
- What a share needs from outside is which of the package and its imports are repo packages (which consts are read), passed in as `repoImports`. A const is read only where it's named, so it's declared in the package or one it imports: a cache key built from its imports' keys covers it. The walk gets only that set, not every repo path, so it can't come to depend on more. Closure, init and local type numbering is already per declaration or file, so a fresh builder gives the same IDs.
- Local type IDs are merged after the walk: an implementation can pair a type and an interface from different packages, and `CallGraph.TypeID` / `FuncID` serve every package. A local type can't be named outside its function, so another package's walk never needs its ID; if it did, `typeID` gives `pkg.Name#?`.
- Implementations are matched from per-package summaries after the merge (next section).
- Tests: each package built alone holds only nodes and edges in its own files, and the shares together are the whole graph; reversing the package order changes nothing; no `#?` IDs; a package's share is the same with every repo path as with only `repoImports` (`store/consts.go` reads `model.MaxTag`).

### Implementations from summaries (M2a)
- A cached package has no type-checker objects, so `types.Implements` can't run on it. Each package stores a `typeSummary` instead: its concrete types' value and pointer method sets, and its interfaces' method sets (embedded ones included), as method key → signature key. Matching reruns every time over all summaries, so a new interface in one package is matched against unchanged types in others.
- Not `gcexportdata` plus `types.Implements`: export data holds package-level declarations only, and local types (`announce.namer`, `spawn$1.job`) take part in implementations.
- Signature keys are equal exactly when the type checker's identity holds: param names dropped, aliases resolved, `byte` as `uint8`, `...T` kept apart from `[]T`, local types by their grepo ID (two local `result`s both print as `store.result`), and unexported method names qualified by their package (`model`'s `seal` isn't `store`'s).
- Interfaces with type terms (`interface{ ~int; String() string }`) are left out, though `types.Implements` matches them: nothing holds a value of one, and caddy, bubbletea and lo have none with methods. Summarizing type sets isn't worth it.
- Summaries are plain strings and maps: they're stored as they are (JSON, like the rest of a share).
- Tests: `model/impls.go` and `store/impls.go` hold one case per rule above; matching from summaries, after a JSON round trip, must give exactly what `types.Implements` gives on the fixture (the old matcher, kept in the test as the oracle).

### Loading (M2a)
- Our own loader: `go list` metadata plus `go/types` directly, replacing `go/packages`. Dependencies are type-checked from source, not compiled.
- Why not compiler export data (`go list -export`): it compiles dependencies with code generation the walker never uses. Cold caddy: ~34s compiling vs ~1–2s type-checking from source; downloads are the same either way (caddy, empty module cache: 160 modules in 11s; the whole first run 12.8s). The first download is logged, since a silent wait looks hung.
- Why not `go/packages` with `NeedDeps`: it can't take cached types, so every load re-type-checks all dependencies, and it keeps syntax and `TypesInfo` for every one of them.
- Dependencies: function bodies dropped before type-checking (dependents only need declarations); only their types are kept; errors in them are ignored (dropping bodies leaves unused imports).
- Each package's export data (`gcexportdata`) is cached next to its graph entry, so unchanged packages are imported from the cache and an edit re-checks only the changed package and its reverse imports.
- The importer decides what a missing package becomes, so the partial pass (M2c) uses the same path.
- Given up: reusing the user's warm `go build` cache, and the Go tooling quirks `go/packages` handles (build tags, cgo files, vendor, `go.work`, overlays) that are now ours to get right through `go list`.
- To check: memory on large repos, and per-package errors (cgo especially).
- `graph` reads `load.Package`, never `go/packages` (#31). `vta-diff` stays on `go/packages` through `load.GoPackages`, as an independent check.
- `go list -json -deps -e ./...`, no `-compiled`: cgo files are type-checked as written with a fake `"C"` (`types.Config.FakeImportC`), so no C compiler is needed and positions stay in real files. In caddy, bubbletea and lo, cgo is only in stdlib dependencies, whose bodies and errors are dropped.
- Matched to `go/packages`: absolute file paths (positions), sizes from the target's `GOARCH` (`unsafe.Sizeof` consts end up in the output), and each module's `go` version (which language features type-check).
- Not matched: `go/packages` adds the compiler's output as one more error on a broken package, repeating its type errors. We don't compile, so it's gone.
- Not matched: an interface defined from another named interface (`type List metav1.ListInterface`). Checked from source, its methods are the other interface's; compiler export data copies the interface, so through `go/packages` they're new methods of `List`, and interface calls through it name a different callee. Source is right. caddy, bubbletea and lo have none; Kubernetes does.
- Stdlib files are parsed under the name `$GOROOT/src/...`, as compiler export data names them: a field of `type T tls.ConnectionState` sits at `$GOROOT/src/crypto/tls/common.go:254`, whatever the install path.
- A package is parsed only once its imports are checked, one per core: only the packages in progress hold syntax trees (caddy peaked at 778 MB when every package parsed up front).
- Measured (`analyze`, same output both ways): empty build cache, caddy 3.7s / 355 MB vs `go/packages` 32.9s / 369 MB; bubbletea 0.7s vs 6.3s; lo 0.7s vs 4.1s. Warm, `go/packages` reads compiled export data and is a little ahead (caddy 1.1s / 226 MB vs 1.3s / 358 MB); cached export data (step 4) covers that case.
- `analyze --loader=packages` (hidden) loads through `go/packages`, for comparing the two on new repos.
- Tests: `TestStdlibPositions` pins the `$GOROOT` naming for both loaders. `internal/load/testdata/mod` adds what `testdata/simple` lacks (a dependency outside the module through `replace`, a build-tagged file, a test file, `unsafe` sizes, `go 1.21` with a go1.22 feature, type errors). On both fixtures, the loader must describe exactly as `go/packages` does: packages, files, imports, errors, scope, file versions, and every `Defs`, `Uses`, `Implicits`, `Selections`, `Instances`, `Types` and `Scopes` entry, by position.

### Store (M2a)
- Goal: unchanged packages aren't type-checked again. A dependency's types come from its cached export data; an unchanged repo package's types too, with its graph share, so it isn't parsed, checked or walked.
- Two PRs. First, dependencies (#34): their export data is cached, repo packages are still checked and walked every run (they need syntax and `TypesInfo`). Then repo packages: their export data and share are cached, and only changed ones and their reverse imports are parsed.
- `internal/store`: one shared SQLite database, `os.UserCacheDir()/grepo/store.db`, through `modernc.org/sqlite` (pure Go: no C compiler, like the loader). Keys hold no machine paths beyond `GOMODCACHE` and, for packages whose files are hashed, the repo root (below), so the stdlib and common modules are shared across repos. WAL and a busy timeout for concurrent runs.
- One table, one row per key: `key`, `import_path`, `last_used`, `export` (`gcexportdata`), and for repo packages `share`: the walk's lists and type summary as JSON, without what's keyed by pointers (local type and closure IDs, which only `vta-diff` uses, and it doesn't use the store). No compression until a measurement asks for it.
- `load` reads and writes export data, and reads a repo package's share with it: a repo package comes from the store only when its row has both, since without a share it must be walked, and walking needs its syntax. It then has `Types` (its exported API only, what importers need), `Key` and `Share`, and no `Syntax` or `TypesInfo`. `graph` decodes shares and encodes the ones it walks (`BuildCallGraph` returns them; `load` can't import `graph`), and `analyze` saves them with `SaveShares` after building. Rows are written in one transaction at the end of a load, shares in another after the graph.
- A repo package checked again because its row has no share (the graph's save failed, or it hasn't run) doesn't rewrite the row: its key fixes what's in it.
- Key (chained: a package's key holds its imports' keys, so a change anywhere below rebuilds it):
  - grepo's version: a hash of its binary. Always right, including the `go/types` it's built with; an upgrade or rebuild means a cold cache.
  - Load config: `GOOS`, `GOARCH` (type sizes), `GOVERSION`, `GOMODCACHE`.
  - The package: `ImportPath`, role (repo package or dependency), module `GoVersion` (`go.mod` isn't one of its files), resolved `Imports`, `ImportMap`.
  - Files: sorted paths relative to the repo root (moving a directory or a local `replace` changes them), plus each file's sha256; for a module-cache package (`Dir` inside `GOMODCACHE`) `module@version` instead of contents, the replacement's when replaced; for the stdlib `GOVERSION`, contents when it's a `devel` toolchain. File names are always in, so build tags and `GOOS` still change a cached package's key.
  - Not in it: `CC` and cgo flags (cgo files are checked as written), `GOPROXY` and friends (versions are fixed by `go.sum`), the `GOROOT` path (stdlib files are `$GOROOT/src/...`).
- Why chained, not cut off at unchanged export data: on caddy, an edit's median rebuild is 5 of 49 repo packages (1.8% of its code); the worst, near the root, is 44. Rebuilding those reads every dependency from the cache, so it costs less than today's whole warm run (1.3s). Cutting off would need every transitive import's export hash in the key (shallow export data names a type two packages down without its methods) and positions questions settled. Revisit on much larger repos.
- `GOMODCACHE` is in the key for now: export data names module-cache files by absolute path (`/home/u/go/pkg/mod/...`), and a field of `type T dep.S` gets its position from there. Naming them `$GOMODCACHE/...` instead, like `$GOROOT`, would make rows portable across machines (M2c's imported graphs); until then they're shared only within one module cache.
- The repo root is in the key of every package whose files are hashed (repo packages, a local `replace`, vendor). The graph shows their positions relative to the root, but export data keeps them absolute: with the same code copied elsewhere, a field of `type T dep.S` read back from the first copy's row printed as `../A/dep/dep.go:3`. Two copies of a repo don't share those rows; the stdlib's and the module cache's they do.
- Packages with errors (a `go list` error, or type errors in a repo package) aren't cached: rebuilt every run. Their messages carry absolute paths, and they're the packages being worked on. Dependencies' errors are dropped anyway, so they're cached.
- A package declaring an interface defined from another named interface isn't cached either (`borrowsMethods`): export data copies the interface, so read back its methods would be new ones, and interface calls through it would name a different callee than a cold load (Kubernetes' `meta.List`, `EvictionIndexInformer`: 60 of its 2,212 dependencies). A package that only mentions such a type is cached: reading its export data keeps the declaring package's checked types. Fields aren't affected: field IDs come from the named type on the selection path, not the field object.
- Export data holds a package's exported declarations, positions included (files as the fileset names them). Written for every caddy package (dropped bodies, errors) in 1.8s serially, read back in 0.25s, in dependency order into one map, so every package's types share objects.
- Cached packages are read in dependency order, like checked ones, under one lock (the importer map is shared).
- `--no-cache` on `analyze` skips the store, for comparing with a cold load.
- Measured with dependencies cached (#34; `analyze`, output byte-identical with `--no-cache`, an empty store and a warm one; best of three): caddy 1.30s → 1.02s warm (load 1.02s → 0.76s; 49 of 1,037 packages checked), bubbletea 0.38s → 0.28s, lo 0.42s → 0.30s. Filling an empty store costs caddy 0.75s once (2.07s). What's left of caddy's warm load: `go list` 0.35–0.40s and `go env` 0.08s (they could run at once), checking its 49 packages, and reading 988 export data blobs under one lock (~0.25s). The store held 22 MB after the three repos, which share their stdlib rows: bubbletea's first run checked 22 of 154.
- Kubernetes (vendored, 1,473 repo packages of 3,685): load 4.0s without the store, 8.6s filling it, 4.0–4.2s warm (2,152 cached); `analyze` 9.3s → 8.7–8.9s; peak RSS ~2.3 GB either way; 262 MB store. No gain: `go list` is 2.3s of the load, its own packages are most of the checking, and reading export data is serial (1.0s, 259 MB: export data is deep, each package repeating its dependencies' declarations) where checking dependencies without bodies ran on every core. GC is the largest CPU cost. Ideas, measured first: read export data in parallel (each read only touches its finished dependencies), shallow export data, cache `go list`'s output.
- Measured with repo packages cached too (`analyze`, output byte-identical with `--no-cache` filling a store and twice warm): caddy 0.87s warm (5 of 1,037 checked: dependencies never cached, like protobuf's `protoreflect`), bubbletea 0.27–0.29s, lo 0.26s (0 checked). Kubernetes 8.1–8.3s warm (60 of 3,685 checked): the call graph is now decoding (1.1–1.2s, was 3.7–4.7s walking), but the load isn't faster (~5.0s): its 3,625 deep export data blobs are read one at a time under one lock, which costs about what checking its 1,473 repo packages on every core did. Filling the store: 24s (16.1s load) against 14s without it; 975 MB, repo packages' export data being most of the growth.
- Later: reading export data in parallel (each read only touches its finished dependencies), first for Kubernetes' warm load; the cost of filling the store (encoding export data or writing it: not measured apart); pruning by `last_used`; caching file hashes by path, mtime and size; the cut-off.
- Tests: `internal/store` round trips, replacing, marking used, two connections saving at once, shares (saved with a row or later, kept when the row is saved again without one, none without a row) and an old schema dropped. `testdata/mod` copied to a temp dir: a load through the store describes like one without; the next checks only repo packages. After each edit to `dep` (a body, a new method, its `go` version, its `replace` moved), a load through the store describes like one without and checks only the repo packages and `dep`. `GOPROXY` reuses every row; the repo's location reuses the stdlib's and module cache's but not hashed packages', whose positions must stay in the copy loaded; another `GOARCH` reuses none. Each key input changes the key (`TestPackageKey`); `fileIdentity` covers the stdlib, a `devel` toolchain, the module cache, a replacement, a local `replace`, vendor and a path beside the module cache. Repo packages (`testdata/mod`): one with a row and a share comes from the store with its types and share and no syntax, the broken one has no row; editing it or its dependency checks it again under a new key; a row without a share isn't rewritten. Graphs (`testdata/simple`): built from the store, byte-identical to walked; each of the 7 sets of packages walked while the rest come from the store (store embeds model's unexported `seal`, reads its alias and const) gives the walked graph; so does a build after each of nine edits (a body in each package, a field, a method making a type implement an interface in the other package and the reverse, a removed file, a new repo import, a file excluded by build tags), checking only the edited packages and their importers; a share that can't be decoded fails the build, naming the package. On caddy, lo and Kubernetes, dropping a random half of a repo's shares from a filled store and running again gives `--no-cache`'s output (Kubernetes: ~800 packages walked against imports from export data, three times).

### Process
- Fixture-first: failing tests before implementation; fixture additions are appended so existing line numbers don't move.
- Dispatch functions per node kind (`handleGenDecl` → `handleTypeSpec` / `handleValueSpec`); helpers are extracted only when they remove real noise.
- Short code comments.
- `grepo vta-diff` checks the call graph against VTA on real repos, as a report rather than a pass/fail test.

## 7. Open items

| Item | Status |
|---|---|
| #8: type arguments on generic calls | deferred to M7 |
| Closures through locals | `inc := func(){…}; inc()` stays dynamic; the closure node exists, but nothing links the call to it |
| #14: implementations of outside interfaces | open |
| Constraint interfaces | interfaces with type terms get no implementations (Implementations from summaries) |
| Aliasing through locals | `m := s.users; m[k] = v` writes into `Store.users` but gets no `content` edge; the `escape` at `m := s.users` makes the gap visible |
| Pointer contents | `s.db.Close()`, `f(s.db)` may change what `db` points to; no edge by design (only direct writes like `s.db.x = 1` give `content`) |
| Method summaries | whether a method changes its receiver, transitively; deferred, needs the root of each write on `Access` |
| Type strings of local types | `types.TypeString` prints a local `result` as `pkg.result`, so a param or field of a local type doesn't show its `#n` ID |
| M6: explanation cache | key on a hash of the node's source, not its ID: a stable ID doesn't mean unchanged code |
