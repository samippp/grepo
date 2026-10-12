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
└── Implementations []Implementation   edges: type → interface
(+ PackageGraph: package nodes and import edges)
```

### Nodes

**`Type`**: every named type in the repo, one struct with a kind.

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

**`Function`**: plain functions, methods and interface methods.

```
ID, Name, Package, Receiver (*Param, nil if none), Params, Results, Exported, Pos
Owner → ID of the type that declares it (methods only)
```

Interface methods are `Function` nodes with no `Receiver`, `Owner` set to the interface, and no calls.

**`Variable`**: package-level `var` and `const`.

```
ID ("pkg.name"), Name, Package, Type, Exported, Pos
Const → flag, like C's const qualifier
Value → consts only, exact (constant.Value.ExactString)
```

### Edges

**`FunctionCall`**: one per call site.

```
Caller          a function ID, or a package path for package-level calls
Callee          a function ID; "" for dynamic calls
CallerPosition  "file:line" of the call (start of the call expression)
CallKind        static | interface | dynamic
Args            []{Expr as written, Type}
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
  for each package → file → declaration:
    FuncDecl → handleFuncDecl      Function node (ID via assignFuncDeclID, Owner via receiverOwner)
                └ addCalls  ast.Inspect the body → callee() / callKind() / args()
    GenDecl  → handleGenDecl       switch on token:
                ├ TYPE      → handleTypeSpec   Type node; struct → structFields;
                │                              interface → embeds + addInterfaceMethod;
                │                              collects concrete / interface objects
                └ VAR/CONST → handleValueSpec  Variable nodes; calls in values with
                                               the package as caller
  after the walk:
    addImplementations   method-name index → candidates → types.Implements
    sort every list
```

## 5. ID rules

| Thing | Format | Example |
|---|---|---|
| Function | `FullName()` | `example.com/simple.greet` |
| Method | `(receiver).name`, receiver = the declaring type's ID (`graph.FuncID`) | `(*…store.Store).Get` |
| Interface method | `(interface).name` | `(…store.Getter).Get` |
| `init` | `pkg.init@file#n`, numbered per file | `…simple.init@main.go#2` |
| Type | `pkg.Name` (`graph.TypeID`); generics keep param names, not constraints; built-ins have no package | `…simple.List[T]`, `error` |
| Field | type ID + `.` + name | `…store.Store.users` |
| Variable | `pkg.name` | `…store.defaultStore` |
| Position | `file:line`, relative to the repo root | `store/store.go:23` |

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

### Functions and calls
- A method is a `Function` with a receiver, not a separate struct.
- One edge per call site. Calls inside closures belong to the enclosing function.
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
- Computed after the walk, from type-checker objects kept during it, so it stays consistent with what the walk records.
- A method-name index (first method as key) picks candidates; `types.Implements` decides.

### Variables
- `var` and `const` share one struct, with a `Const` flag. Consts store exact values; untyped consts keep `untyped int`.
- Blank (`_`) variables get no node, but calls in their values are still recorded.
- Package-level calls use the package as the caller: an existing node, nothing synthetic.

### Process
- Fixture-first: failing tests before implementation; fixture additions are appended so existing line numbers don't move.
- Dispatch functions per node kind (`handleGenDecl` → `handleTypeSpec` / `handleValueSpec`); helpers are extracted only when they remove real noise.
- Short code comments.
- `grepo vta-diff` checks the call graph against VTA on real repos, as a report rather than a pass/fail test.

## 7. Open items

| Item | Status |
|---|---|
| #8: type arguments on generic calls | deferred to M5 |
| #13: closure nodes | open; likely with M5 |
| #14: implementations of outside interfaces | open |
| Types declared inside functions | not walked: no node, so calls to their methods have a callee with no node (e.g. caddy's `acmeCapable`) |
