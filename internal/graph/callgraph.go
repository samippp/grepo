package graph

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

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
	Owner    string  `json:"owner,omitempty"` // methods: ID of the type that declares it
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
	Caller         string   `json:"caller"`
	Callee         string   `json:"callee"`
	CallerPosition string   `json:"callerPosition"`
	CallKind       CallKind `json:"callKind"`
	Args           []Arg    `json:"args"`
}

// Arg is one argument at a call site.
type Arg struct {
	Expr string `json:"expr"` // as written, e.g. "u.Name"
	Type string `json:"type"` // e.g. "string"
}

type CallKind string

const (
	StaticCall    CallKind = "static"
	InterfaceCall CallKind = "interface"
	DynamicCall   CallKind = "dynamic"
)

// CallGraph holds the repo's functions and the calls made inside them.
type CallGraph struct {
	Types     []Type         `json:"types"`
	Functions []Function     `json:"functions"`
	Calls     []FunctionCall `json:"calls"`
}

// BuildCallGraph records every function and call in res. Rules (see tests):
//   - IDs from FullName(), types from types.TypeString(t, nil)
//   - closure calls belong to the enclosing func; built-ins/conversions aren't calls
//   - external calls kept; interface calls point at the interface method
//   - dynamic calls have no Callee; one edge per call site; output sorted
func BuildCallGraph(res *load.Result) *CallGraph {
	b := &callGraphBuilder{
		root:      res.Dir,
		inits:     map[string]int{},
		types:     []Type{},
		functions: []Function{},
		calls:     []FunctionCall{},
	}

	for _, pkg := range res.Packages {
		b.pkg = pkg
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
	}
	slices.SortFunc(b.types, func(a, b Type) int {
		return cmp.Compare(a.ID, b.ID)
	})
	slices.SortFunc(b.functions, func(a, b Function) int {
		return cmp.Compare(a.ID, b.ID)
	})
	slices.SortFunc(b.calls, func(a, b FunctionCall) int {
		return cmp.Or(
			cmp.Compare(a.Caller, b.Caller),
			cmp.Compare(a.CallerPosition, b.CallerPosition),
			cmp.Compare(a.Callee, b.Callee),
		)
	})
	return &CallGraph{Types: b.types, Functions: b.functions, Calls: b.calls}
}

// callGraphBuilder holds walk context and results.
type callGraphBuilder struct {
	root string            // repo root
	pkg  *packages.Package // current package

	inits map[string]int // init funcs seen per file, for unique init IDs

	types     []Type
	functions []Function
	calls     []FunctionCall
}

func (b *callGraphBuilder) handleFuncDecl(funcDecl *ast.FuncDecl) {
	info := b.pkg.TypesInfo

	// Missing only if the package has type errors; skip it.
	fn, ok := info.Defs[funcDecl.Name].(*types.Func)
	if !ok {
		return
	}

	var recv *Param
	if rs := handleFields(funcDecl.Recv, info); len(rs) > 0 {
		recv = &rs[0]
	}

	id := b.funcID(funcDecl, fn)
	b.functions = append(b.functions, Function{
		ID:       id,
		Name:     funcDecl.Name.Name,
		Package:  b.pkg.PkgPath,
		Receiver: recv,
		Params:   handleFields(funcDecl.Type.Params, info),
		Results:  handleFields(funcDecl.Type.Results, info),
		Exported: funcDecl.Name.IsExported(),
		Pos:      b.pos(funcDecl.Pos()),
		Owner:    receiverOwner(fn),
	})

	// A nil *BlockStmt passed as ast.Node isn't == nil, so check here.
	if funcDecl.Body != nil {
		b.handleFuncCalls(id, funcDecl.Body)
	}
}

// funcID is fn's FullName, except each init gets "pkg.init@file#n", since a
// package can have several. Call once per declaration: it counts inits.
func (b *callGraphBuilder) funcID(funcDecl *ast.FuncDecl, fn *types.Func) string {
	if funcDecl.Recv != nil || funcDecl.Name.Name != "init" {
		return fn.FullName()
	}
	file := rel(b.root, b.pkg.Fset.Position(funcDecl.Pos()).Filename)
	b.inits[file]++
	return fmt.Sprintf("%s@%s#%d", fn.FullName(), file, b.inits[file])
}

// handleFuncCalls records every call under root, made by the function callerID.
func (b *callGraphBuilder) handleFuncCalls(callerID string, root ast.Node) {
	ast.Inspect(root, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee, isCall := b.callee(call)
		if !isCall {
			return true
		}
		c := FunctionCall{
			Caller:         callerID,
			CallerPosition: b.pos(call.Pos()),
			CallKind:       callKind(callee),
			Args:           b.args(call.Args),
		}
		if callee != nil {
			c.Callee = callee.FullName()
		}
		b.calls = append(b.calls, c)
		return true // closures and arguments can contain calls
	})
}

// callee returns the function call refers to, or nil if unknown (dynamic).
// isCall is false for conversions, built-ins and inline closures.
func (b *callGraphBuilder) callee(call *ast.CallExpr) (fn *types.Func, isCall bool) {
	info := b.pkg.TypesInfo
	if tv := info.Types[call.Fun]; tv.IsType() || tv.IsBuiltin() {
		return nil, false // int64(n), len(x)
	}

	var name *ast.Ident
	switch fun := unindex(ast.Unparen(call.Fun)).(type) {
	case *ast.Ident:
		name = fun // foo(), f(), first[int](), handlers[0]()
	case *ast.SelectorExpr:
		name = fun.Sel // pkg.Foo(), obj.Method(), slices.Map[int]()
	case *ast.FuncLit:
		return nil, false // func(){...}(): its body is walked anyway
	}
	// Other shapes (f()()) leave name nil: dynamic.

	fn, _ = info.Uses[name].(*types.Func)
	if fn != nil {
		fn = fn.Origin() // List[int].Push → List[T].Push, to match the declared node
	}
	return fn, true
}

// unindex strips type args or an index: first[int] → first, handlers[0] → handlers.
func unindex(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.IndexExpr:
		return x.X
	case *ast.IndexListExpr:
		return x.X
	}
	return e
}

// callKind classifies a call by its callee.
func callKind(callee *types.Func) CallKind {
	if callee == nil {
		return DynamicCall
	}
	if recv := callee.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
		return InterfaceCall
	}
	return StaticCall
}

// args describes each argument expression at a call site.
func (b *callGraphBuilder) args(exprs []ast.Expr) []Arg {
	var args []Arg
	for _, e := range exprs {
		args = append(args, Arg{
			Expr: types.ExprString(e),
			Type: types.TypeString(b.pkg.TypesInfo.TypeOf(e), nil),
		})
	}
	return args
}

// handleGenDecl dispatches a type, var or const declaration to its spec handler.
func (b *callGraphBuilder) handleGenDecl(genDecl *ast.GenDecl) {
	switch genDecl.Tok {
	case token.TYPE:
		for _, spec := range genDecl.Specs {
			b.handleTypeSpec(spec.(*ast.TypeSpec)) // TYPE decls only hold TypeSpecs
		}
	case token.VAR, token.CONST:
		// TODO: variables, and package-level calls (#7)
	}
	// token.IMPORT: nothing to record.
}

// handleTypeSpec records one named type, plus its interface methods.
func (b *callGraphBuilder) handleTypeSpec(spec *ast.TypeSpec) {
	tn, ok := b.pkg.TypesInfo.Defs[spec.Name].(*types.TypeName)
	if !ok {
		return
	}
	t := Type{
		ID:       typeID(tn),
		Name:     tn.Name(),
		Package:  b.pkg.PkgPath,
		Exported: tn.Exported(),
		Pos:      b.pos(spec.Pos()),
	}

	if alias, ok := tn.Type().(*types.Alias); ok {
		t.Kind = AliasType
		t.Target = types.TypeString(alias.Rhs(), nil)
		b.types = append(b.types, t)
		return
	}

	switch u := tn.Type().Underlying().(type) {
	case *types.Struct:
		t.Kind, t.Fields = StructType, b.structFields(t.ID, u)
	case *types.Interface:
		t.Kind = InterfaceType
		for e := range u.EmbeddedTypes() {
			t.Embeds = append(t.Embeds, types.TypeString(e, nil))
		}
		for m := range u.ExplicitMethods() {
			b.handleInterfaceMethod(m, t.ID)
		}
	default:
		t.Kind, t.Underlying = OtherType, types.TypeString(u, nil)
	}
	b.types = append(b.types, t)
}

// structFields describes each field of st, a struct owned by the type ownerID.
func (b *callGraphBuilder) structFields(ownerID string, st *types.Struct) []Field {
	var fields []Field
	for f := range st.Fields() {
		fields = append(fields, Field{
			ID:       ownerID + "." + f.Name(),
			Name:     f.Name(),
			Type:     types.TypeString(f.Type(), nil),
			Embedded: f.Embedded(),
			Pos:      b.pos(f.Pos()),
		})
	}
	return fields
}

// handleInterfaceMethod records a method declared in an interface as a
// Function owned by that interface. It has no body, so no calls.
func (b *callGraphBuilder) handleInterfaceMethod(m *types.Func, ownerID string) {
	sig := m.Signature()
	b.functions = append(b.functions, Function{
		ID:       m.FullName(),
		Name:     m.Name(),
		Package:  b.pkg.PkgPath,
		Params:   tupleParams(sig.Params()),
		Results:  tupleParams(sig.Results()),
		Exported: m.Exported(),
		Pos:      b.pos(m.Pos()),
		Owner:    ownerID,
	})
}

// receiverOwner is the ID of the type a method is declared on, or "" for a
// plain function. *Store and aliases of Store both give Store's ID.
func receiverOwner(fn *types.Func) string {
	recv := fn.Signature().Recv()
	if recv == nil {
		return ""
	}
	t := recv.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return "" // only possible in code with type errors
	}
	return typeID(named.Obj())
}

// typeID is "pkg.Name", with type param names for generics: "pkg.List[T]".
// (types.TypeString would give "pkg.List[T any]", which method IDs don't use.)
func typeID(tn *types.TypeName) string {
	id := tn.Pkg().Path() + "." + tn.Name()
	named, ok := tn.Type().(*types.Named)
	if !ok || named.TypeParams().Len() == 0 {
		return id
	}
	var params []string
	for tp := range named.TypeParams().TypeParams() {
		params = append(params, tp.Obj().Name())
	}
	return id + "[" + strings.Join(params, ", ") + "]"
}

// tupleParams converts a params or results tuple into Params.
func tupleParams(t *types.Tuple) []Param {
	var params []Param
	for v := range t.Variables() {
		params = append(params, Param{Name: v.Name(), Type: types.TypeString(v.Type(), nil)})
	}
	return params
}

// pos formats p as "file:line" relative to the repo root.
func (b *callGraphBuilder) pos(p token.Pos) string {
	position := b.pkg.Fset.Position(p)
	return rel(b.root, position.Filename) + ":" + strconv.Itoa(position.Line)
}

// Parses a field list into a list of Params
func handleFields(fields *ast.FieldList, info *types.Info) []Param {
	if fields == nil {
		return nil
	}
	var params []Param
	for _, f := range fields.List {
		typ := types.TypeString(info.TypeOf(f.Type), nil)

		if len(f.Names) == 0 { // f.Names is []*ast.Ident
			params = append(params, Param{Type: typ})
			continue
		}
		for _, name := range f.Names { // name is *ast.Ident
			params = append(params, Param{Name: name.Name, Type: typ})
		}
	}
	return params
}

// CountByKind counts calls per kind. The StaticCall share is the resolution rate.
func (g *CallGraph) CountByKind() map[CallKind]int {
	counts := make(map[CallKind]int)
	for _, c := range g.Calls {
		counts[c.CallKind]++
	}
	return counts
}
