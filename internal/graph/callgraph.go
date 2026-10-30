package graph

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"

	"golang.org/x/tools/go/packages"

	"github.com/samippp/grepo/internal/load"
)

type Function struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Package  string  `json:"package"`
	Receiver *Param  `json:"receiver,omitempty"`
	Params   []Param `json:"params"`
	Results  []Param `json:"results"`
	Exported bool    `json:"exported"`
	Pos      string  `json:"pos"`
	Owner    string  `json:"owner,omitempty"`  // methods: ID of the type that declares it
	Parent   string  `json:"parent,omitempty"` // closures: the enclosing function or closure, or the package var
}

// Type is a named type declared in the repo.
type Type struct {
	ID       string   `json:"id"` // generics keep param names: "pkg.List[T]"
	Name     string   `json:"name"`
	Package  string   `json:"package"`
	Kind     TypeKind `json:"kind"`
	Exported bool     `json:"exported"`
	Pos      string   `json:"pos"`

	// Kind-specific; empty for the other kinds.
	Fields     []Field  `json:"fields,omitempty"`     // struct
	Embeds     []string `json:"embeds,omitempty"`     // interface: embedded interfaces
	Target     string   `json:"target,omitempty"`     // alias: the aliased type
	Underlying string   `json:"underlying,omitempty"` // other: e.g. "int", "func(string)"
}

type TypeKind string

const (
	StructType    TypeKind = "struct"
	InterfaceType TypeKind = "interface"
	AliasType     TypeKind = "alias"
	OtherType     TypeKind = "other"
)

// Variable is a package-level var or const. Blank (_) vars get no node.
type Variable struct {
	ID       string `json:"id"` // "pkg.name"
	Name     string `json:"name"`
	Package  string `json:"package"`
	Type     string `json:"type"` // consts may be untyped: "untyped int"
	Const    bool   `json:"const,omitempty"`
	Value    string `json:"value,omitempty"` // consts only: constant.Value.ExactString()
	Exported bool   `json:"exported"`
	Pos      string `json:"pos"`
}

// Implementation records that a concrete type satisfies an interface. Both
// are repo types. Read it in either direction: what Store implements, or
// what implements Getter.
type Implementation struct {
	Type        string `json:"type"`                  // concrete type ID
	Interface   string `json:"interface"`             // interface type ID
	PointerOnly bool   `json:"pointerOnly,omitempty"` // only *Type implements it
}

// Field is one field of a struct type.
type Field struct {
	ID       string `json:"id"` // owner type ID + "." + name
	Name     string `json:"name"`
	Type     string `json:"type"`
	Embedded bool   `json:"embedded,omitempty"`
	Pos      string `json:"pos"`
}

type Param struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type FunctionCall struct {
	Caller   string   `json:"caller"`
	Callee   string   `json:"callee"`
	Pos      string   `json:"pos"`
	CallKind CallKind `json:"callKind"`
	Args     []Arg    `json:"args"`
	Through  string   `json:"through,omitempty"` // field or package var the call is made on or takes its func from
	Stmt     string   `json:"stmt,omitempty"`    // "go" or "defer" when the call is that statement's
}

// Arg is one argument at a call site.
type Arg struct {
	Expr string `json:"expr"`           // as written, e.g. "u.Name"
	Type string `json:"type"`           // e.g. "string"
	Func string `json:"func,omitempty"` // the function the argument is: a closure, a declared function, a method value
}

type CallKind string

const (
	StaticCall    CallKind = "static"
	InterfaceCall CallKind = "interface"
	DynamicCall   CallKind = "dynamic"
)

// Access is one write, read or escape of a package-level var or a struct field.
type Access struct {
	Accessor string     `json:"accessor"` // function ID, or the package for package-level values
	Target   string     `json:"target"`   // VarID or FieldID, in the repo or not
	Pos      string     `json:"pos"`
	Kind     AccessKind `json:"kind"`
	Func     string     `json:"func,omitempty"` // assign / construct: the function the written value is
}

type AccessKind string

const (
	AssignAccess    AccessKind = "assign"    // x = v
	UpdateAccess    AccessKind = "update"    // x++, x += v, x = f(x)
	ContentAccess   AccessKind = "content"   // x[k] = v, delete(x, k); or a struct value on a written path
	ConstructAccess AccessKind = "construct" // T{f: v}, or a package var's initial value
	ReadAccess      AccessKind = "read"      // the value is loaded: y := x, x.f, x[k]
	EscapeAccess    AccessKind = "escape"    // access is handed out: &x, a pointer method on x, a slice or map passed on
)

// CallGraph holds the repo's functions and the calls made inside them.
type CallGraph struct {
	Types     []Type         `json:"types"`
	Variables []Variable     `json:"variables"`
	Functions []Function     `json:"functions"`
	Calls     []FunctionCall `json:"calls"`
	Accesses  []Access       `json:"accesses"`

	Implementations []Implementation `json:"implementations"`

	ids      idTable                 // local type IDs, for FuncID / TypeID
	closures map[*ast.FuncLit]string // closure IDs, for ClosureID
}

// FuncID is grepo's ID for fn, including methods of this graph's local types.
func (g *CallGraph) FuncID(fn *types.Func) string { return g.ids.funcID(fn) }

// TypeID is grepo's ID for tn, including this graph's local types.
func (g *CallGraph) TypeID(tn *types.TypeName) string { return g.ids.typeID(tn) }

// BuildCallGraph records every function and call in res. Rules (see tests):
//   - IDs from funcID / typeID, types from types.TypeString(t, nil)
//   - closures are nodes owning what's inside them; built-ins/conversions aren't calls
//   - external calls kept; interface calls point at the interface method
//   - dynamic calls have no Callee; one edge per call site; output sorted
func BuildCallGraph(res *load.Result) *CallGraph {
	repo := map[string]bool{}
	for _, pkg := range res.Packages {
		repo[pkg.PkgPath] = true
	}

	g := &CallGraph{
		Types:     []Type{},
		Variables: []Variable{},
		Functions: []Function{},
		Calls:     []FunctionCall{},
		Accesses:  []Access{},
		ids:       idTable{},
		closures:  map[*ast.FuncLit]string{},
	}
	var summaries []typeSummary
	for _, pkg := range res.Packages {
		b := buildPackage(pkg, res.Dir, repo)
		g.Types = append(g.Types, b.types...)
		g.Variables = append(g.Variables, b.variables...)
		g.Functions = append(g.Functions, b.functions...)
		g.Calls = append(g.Calls, b.calls...)
		g.Accesses = append(g.Accesses, b.accesses...)
		maps.Copy(g.ids, b.ids)
		maps.Copy(g.closures, b.closures)
		summaries = append(summaries, b.summary)
	}
	// After the merge: a type and an interface it implements can be in different packages.
	g.Implementations = matchImplementations(summaries)
	g.sort()
	return g
}

// buildPackage walks one package. Its output depends only on pkg, the repo
// root and the repo's package paths, so packages can be built in any order
// (and cached).
func buildPackage(pkg *packages.Package, root string, repo map[string]bool) *callGraphBuilder {
	b := &callGraphBuilder{
		root:       root,
		pkg:        pkg,
		inits:      map[string]int{},
		ids:        idTable{},
		localCount: map[string]int{},
		repo:       repo,
		suppressed: map[ast.Expr]bool{},
		stmt:       map[*ast.CallExpr]string{},

		closures:      map[*ast.FuncLit]string{},
		closureParent: map[*ast.FuncLit]string{},
		closureCount:  map[string]int{},
	}
	for _, syntaxTree := range pkg.Syntax {
		for _, decl := range syntaxTree.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				b.handleFuncDecl(d)
			case *ast.GenDecl:
				b.handleGenDecl(d)
			}
		}
	}
	return b
}

// sort puts every list in a stable order, whatever order packages were built in.
func (g *CallGraph) sort() {
	slices.SortFunc(g.Types, func(x, y Type) int {
		return cmp.Compare(x.ID, y.ID)
	})
	slices.SortFunc(g.Variables, func(x, y Variable) int {
		return cmp.Compare(x.ID, y.ID)
	})
	slices.SortFunc(g.Functions, func(x, y Function) int {
		return cmp.Compare(x.ID, y.ID)
	})
	slices.SortFunc(g.Implementations, func(x, y Implementation) int {
		return cmp.Or(cmp.Compare(x.Type, y.Type), cmp.Compare(x.Interface, y.Interface))
	})
	slices.SortFunc(g.Calls, func(x, y FunctionCall) int {
		return cmp.Or(
			cmp.Compare(x.Caller, y.Caller),
			cmp.Compare(x.Pos, y.Pos),
			cmp.Compare(x.Callee, y.Callee),
		)
	})
	slices.SortFunc(g.Accesses, func(x, y Access) int {
		return cmp.Or(
			cmp.Compare(x.Accessor, y.Accessor),
			cmp.Compare(x.Pos, y.Pos),
			cmp.Compare(x.Target, y.Target),
			cmp.Compare(x.Kind, y.Kind),
		)
	})
}

// callGraphBuilder holds one package's walk context and results.
type callGraphBuilder struct {
	root string            // repo root
	pkg  *packages.Package // current package

	inits      map[string]int           // init funcs seen per file, for unique init IDs
	ids        idTable                  // local type IDs, filled during the walk
	localCount map[string]int           // local types seen per "parent.name", for unique IDs
	repo       map[string]bool          // the repo's package paths: their consts are read
	suppressed map[ast.Expr]bool        // update right sides walk won't read: x = x + 1
	stmt       map[*ast.CallExpr]string // "go" / "defer" for those statements' calls

	closures      map[*ast.FuncLit]string // closure IDs, from numberClosures
	closureParent map[*ast.FuncLit]string
	closureCount  map[string]int // closures numbered per ID prefix

	types     []Type
	variables []Variable
	functions []Function
	calls     []FunctionCall
	accesses  []Access

	summary typeSummary // types by method set, for matchImplementations after the merge
}

// handleGenDecl dispatches a type, var or const declaration to its spec handler.
func (b *callGraphBuilder) handleGenDecl(genDecl *ast.GenDecl) {
	switch genDecl.Tok {
	case token.TYPE:
		for _, spec := range genDecl.Specs {
			spec := spec.(*ast.TypeSpec) // TYPE decls only hold TypeSpecs
			b.handleTypeSpec(spec)
			b.expr(b.pkg.PkgPath, spec.Type) // consts in it: type T [Max]byte
		}
	case token.VAR, token.CONST:
		for _, spec := range genDecl.Specs {
			b.handleValueSpec(spec.(*ast.ValueSpec)) // VAR/CONST decls only hold ValueSpecs
		}
	}
	// token.IMPORT: nothing to record.
}

// tupleParams converts a params or results tuple into Params.
func tupleParams(tuple *types.Tuple) []Param {
	var params []Param
	for v := range tuple.Variables() {
		params = append(params, Param{Name: v.Name(), Type: types.TypeString(v.Type(), nil)})
	}
	return params
}

// pos formats p as "file:line" relative to the repo root.
func (b *callGraphBuilder) pos(p token.Pos) string {
	position := b.pkg.Fset.Position(p)
	return rel(b.root, position.Filename) + ":" + strconv.Itoa(position.Line)
}

// CountByKind counts calls per kind. The StaticCall share is the resolution rate.
func (g *CallGraph) CountByKind() map[CallKind]int {
	counts := make(map[CallKind]int)
	for _, c := range g.Calls {
		counts[c.CallKind]++
	}
	return counts
}
