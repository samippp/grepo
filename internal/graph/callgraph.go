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

// Access is one write, read or escape of a package-level var or a struct field.
type Access struct {
	Accessor string     `json:"accessor"` // function ID, or the package for package-level values
	Target   string     `json:"target"`   // VarID or FieldID, in the repo or not
	Pos      string     `json:"pos"`
	Kind     AccessKind `json:"kind"`
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

	ids idTable // local type IDs, for FuncID / TypeID
}

// FuncID is grepo's ID for fn, including methods of this graph's local types.
func (g *CallGraph) FuncID(fn *types.Func) string { return g.ids.funcID(fn) }

// TypeID is grepo's ID for tn, including this graph's local types.
func (g *CallGraph) TypeID(tn *types.TypeName) string { return g.ids.typeID(tn) }

// BuildCallGraph records every function and call in res. Rules (see tests):
//   - IDs from funcID / typeID, types from types.TypeString(t, nil)
//   - closure calls belong to the enclosing func; built-ins/conversions aren't calls
//   - external calls kept; interface calls point at the interface method
//   - dynamic calls have no Callee; one edge per call site; output sorted
func BuildCallGraph(res *load.Result) *CallGraph {
	b := &callGraphBuilder{
		root:       res.Dir,
		inits:      map[string]int{},
		ids:        idTable{},
		localCount: map[string]int{},
		repo:       map[string]bool{},
		suppressed: map[ast.Expr]bool{},

		types:     []Type{},
		variables: []Variable{},
		functions: []Function{},
		calls:     []FunctionCall{},
		accesses:  []Access{},

		implementations: []Implementation{},
	}

	for _, pkg := range res.Packages {
		b.repo[pkg.PkgPath] = true
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
	b.addImplementations()

	slices.SortFunc(b.types, func(x, y Type) int {
		return cmp.Compare(x.ID, y.ID)
	})
	slices.SortFunc(b.variables, func(x, y Variable) int {
		return cmp.Compare(x.ID, y.ID)
	})
	slices.SortFunc(b.functions, func(x, y Function) int {
		return cmp.Compare(x.ID, y.ID)
	})
	slices.SortFunc(b.implementations, func(x, y Implementation) int {
		return cmp.Or(cmp.Compare(x.Type, y.Type), cmp.Compare(x.Interface, y.Interface))
	})
	slices.SortFunc(b.calls, func(x, y FunctionCall) int {
		return cmp.Or(
			cmp.Compare(x.Caller, y.Caller),
			cmp.Compare(x.Pos, y.Pos),
			cmp.Compare(x.Callee, y.Callee),
		)
	})
	slices.SortFunc(b.accesses, func(x, y Access) int {
		return cmp.Or(
			cmp.Compare(x.Accessor, y.Accessor),
			cmp.Compare(x.Pos, y.Pos),
			cmp.Compare(x.Target, y.Target),
			cmp.Compare(x.Kind, y.Kind),
		)
	})
	return &CallGraph{
		Types:           b.types,
		Variables:       b.variables,
		Functions:       b.functions,
		Calls:           b.calls,
		Accesses:        b.accesses,
		Implementations: b.implementations,
		ids:             b.ids,
	}
}

// callGraphBuilder holds walk context and results.
type callGraphBuilder struct {
	root string            // repo root
	pkg  *packages.Package // current package

	inits      map[string]int    // init funcs seen per file, for unique init IDs
	ids        idTable           // local type IDs, filled during the walk
	localCount map[string]int    // local types seen per "parent.name", for unique IDs
	repo       map[string]bool   // the repo's package paths: their consts are read
	suppressed map[ast.Expr]bool // update right sides walk won't read: x = x + 1

	types     []Type
	variables []Variable
	functions []Function
	calls     []FunctionCall
	accesses  []Access

	// Kept during the walk for computing implementations afterwards.
	concrete   []*types.TypeName
	interfaces []*types.TypeName

	implementations []Implementation
}

func (b *callGraphBuilder) handleFuncDecl(funcDecl *ast.FuncDecl) {
	// Missing only if the package has type errors; skip it.
	fn, ok := b.pkg.TypesInfo.Defs[funcDecl.Name].(*types.Func)
	if !ok {
		return
	}

	id := b.assignFuncDeclID(funcDecl, fn)
	b.functions = append(b.functions, b.newFunction(fn, id))

	// A nil *BlockStmt passed as ast.Node isn't == nil, so check here.
	if funcDecl.Body != nil {
		b.walk(id, funcDecl.Body)
	}
}

// assignFuncDeclID is FuncID(fn), except each init gets "pkg.init@file#n", since a
// package can have several. Call once per declaration: it counts inits.
func (b *callGraphBuilder) assignFuncDeclID(funcDecl *ast.FuncDecl, fn *types.Func) string {
	if funcDecl.Recv != nil || funcDecl.Name.Name != "init" {
		return b.ids.funcID(fn)
	}
	file := rel(b.root, b.pkg.Fset.Position(funcDecl.Pos()).Filename)
	b.inits[file]++
	return fmt.Sprintf("%s@%s#%d", fn.FullName(), file, b.inits[file])
}

// addCall records one call made by callerID.
func (b *callGraphBuilder) addCall(callerID string, call *ast.CallExpr) {
	callee, isCall := b.callee(call)
	if !isCall {
		return
	}
	edge := FunctionCall{
		Caller:   callerID,
		Pos:      b.pos(call.Pos()),
		CallKind: callKind(callee),
		Args:     b.args(call.Args),
		Through:  b.through(call.Fun),
	}
	if callee != nil {
		edge.Callee = b.ids.funcID(callee)
	}
	b.calls = append(b.calls, edge)
}

// assignLocalTypeID gives a type declared inside parentID the ID
// "parent.name#n", numbered per name in source order. Call once per declaration.
func (b *callGraphBuilder) assignLocalTypeID(parentID string, spec *ast.TypeSpec) {
	tn, ok := b.pkg.TypesInfo.Defs[spec.Name].(*types.TypeName)
	if !ok {
		return
	}
	key := parentID + "." + tn.Name()
	b.localCount[key]++
	b.ids[tn] = fmt.Sprintf("%s#%d", key, b.localCount[key])
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
	for _, arg := range exprs {
		args = append(args, Arg{
			Expr: types.ExprString(arg),
			Type: types.TypeString(b.pkg.TypesInfo.TypeOf(arg), nil),
		})
	}
	return args
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

// handleValueSpec records each var or const in spec, plus the calls and
// accesses in its values, which run at package startup so their caller is the package (#7).
func (b *callGraphBuilder) handleValueSpec(spec *ast.ValueSpec) {
	for _, name := range spec.Names {
		if name.Name == "_" {
			continue // blank: no node, but its value's calls are still recorded below
		}
		obj := b.pkg.TypesInfo.Defs[name]
		if obj == nil {
			continue
		}
		node := Variable{
			ID:       VarID(obj),
			Name:     obj.Name(),
			Package:  b.pkg.PkgPath,
			Type:     types.TypeString(obj.Type(), nil),
			Exported: obj.Exported(),
			Pos:      b.pos(name.Pos()),
		}
		if cnst, ok := obj.(*types.Const); ok {
			node.Const, node.Value = true, cnst.Val().ExactString()
		} else if len(spec.Values) > 0 {
			b.accesses = append(b.accesses, Access{Accessor: b.pkg.PkgPath, Target: node.ID, Pos: b.pos(name.Pos()), Kind: ConstructAccess}) // its initial value
		}
		b.variables = append(b.variables, node)
	}

	// Only the values: walking the whole spec would also visit its type expression.
	for _, value := range spec.Values {
		b.handed(b.pkg.PkgPath, value)
	}
}

// handleTypeSpec records one named type, plus its interface methods.
func (b *callGraphBuilder) handleTypeSpec(spec *ast.TypeSpec) {
	tn, ok := b.pkg.TypesInfo.Defs[spec.Name].(*types.TypeName)
	if !ok {
		return
	}
	node := Type{
		ID:       b.ids.typeID(tn),
		Name:     tn.Name(),
		Package:  b.pkg.PkgPath,
		Exported: tn.Exported(),
		Pos:      b.pos(spec.Pos()),
	}

	if alias, ok := tn.Type().(*types.Alias); ok {
		node.Kind = AliasType
		node.Target = types.TypeString(alias.Rhs(), nil)
		b.types = append(b.types, node)
		return
	}

	switch u := tn.Type().Underlying().(type) {
	case *types.Struct:
		node.Kind, node.Fields = StructType, b.structFields(node.ID, u)
	case *types.Interface:
		node.Kind = InterfaceType
		for embedded := range u.EmbeddedTypes() {
			node.Embeds = append(node.Embeds, types.TypeString(embedded, nil))
		}
		for fn := range u.ExplicitMethods() {
			b.addInterfaceMethod(fn)
		}
	default:
		node.Kind, node.Underlying = OtherType, types.TypeString(u, nil)
	}
	b.types = append(b.types, node)

	// Kept for addImplementations. Generics can't be checked without type args.
	if !isGeneric(tn) {
		if node.Kind == InterfaceType {
			b.interfaces = append(b.interfaces, tn)
		} else {
			b.concrete = append(b.concrete, tn)
		}
	}
}

// addImplementations records which concrete repo types satisfy which repo
// interfaces. It runs after the walk: the two can be in different packages.
// Each type is only checked against interfaces indexed under one of its methods.
func (b *callGraphBuilder) addImplementations() {
	byMethod := indexInterfaces(b.interfaces)
	for _, tn := range b.concrete {
		for _, entry := range candidateInterfaces(tn, byMethod) {
			value := types.Implements(tn.Type(), entry.iface)
			if !value && !types.Implements(types.NewPointer(tn.Type()), entry.iface) {
				continue
			}
			b.implementations = append(b.implementations, Implementation{
				Type:        b.ids.typeID(tn),
				Interface:   b.ids.typeID(entry.name),
				PointerOnly: !value, // *T always has T's methods, so only the reverse can fail
			})
		}
	}
}

// ifaceEntry is an interface in the method index.
type ifaceEntry struct {
	name  *types.TypeName  // for its ID
	iface *types.Interface // for types.Implements
}

// indexInterfaces files each interface under its first method name: a type
// without that method can't implement it. Empty interfaces are skipped, since
// every type implements them.
func indexInterfaces(interfaces []*types.TypeName) map[string][]ifaceEntry {
	byMethod := map[string][]ifaceEntry{}
	for _, tn := range interfaces {
		iface := tn.Type().Underlying().(*types.Interface)
		if iface.NumMethods() == 0 {
			continue
		}
		key := iface.Method(0).Name() // methods are sorted, so this is stable
		byMethod[key] = append(byMethod[key], ifaceEntry{tn, iface})
	}
	return byMethod
}

// candidateInterfaces returns the interfaces tn might implement. It looks at
// *tn's method set, which includes value-receiver and promoted methods. Each
// interface has one key, so none is returned twice.
func candidateInterfaces(tn *types.TypeName, byMethod map[string][]ifaceEntry) []ifaceEntry {
	var out []ifaceEntry
	for sel := range types.NewMethodSet(types.NewPointer(tn.Type())).Methods() {
		out = append(out, byMethod[sel.Obj().Name()]...)
	}
	return out
}

// isGeneric reports whether tn has type parameters, like List[T].
func isGeneric(tn *types.TypeName) bool {
	named, ok := tn.Type().(*types.Named)
	return ok && named.TypeParams().Len() > 0
}

// structFields describes each field of st, a struct owned by the type ownerID.
func (b *callGraphBuilder) structFields(ownerID string, st *types.Struct) []Field {
	var fields []Field
	for field := range st.Fields() {
		fields = append(fields, Field{
			ID:       FieldID(ownerID, field.Name()),
			Name:     field.Name(),
			Type:     types.TypeString(field.Type(), nil),
			Embedded: field.Embedded(),
			Pos:      b.pos(field.Pos()),
		})
	}
	return fields
}

// addInterfaceMethod records a method declared in an interface as a
// Function owned by that interface. It has no body, so no calls.
func (b *callGraphBuilder) addInterfaceMethod(fn *types.Func) {
	node := b.newFunction(fn, b.ids.funcID(fn))
	node.Receiver = nil // interface methods have no written receiver
	b.functions = append(b.functions, node)
}

// newFunction builds the Function node for fn, with the given ID.
func (b *callGraphBuilder) newFunction(fn *types.Func, id string) Function {
	sig := fn.Signature()
	node := Function{
		ID:       id,
		Name:     fn.Name(),
		Package:  b.pkg.PkgPath,
		Params:   tupleParams(sig.Params()),
		Results:  tupleParams(sig.Results()),
		Exported: fn.Exported(),
		Pos:      b.pos(fn.Pos()),
		Owner:    b.ids.receiverOwner(fn),
	}
	if recv := sig.Recv(); recv != nil {
		node.Receiver = &Param{Name: recv.Name(), Type: types.TypeString(recv.Type(), nil)}
	}
	return node
}

// idTable holds the IDs of local types (declared inside a function), which
// the walk assigns. Every other ID is computed from the object alone.
type idTable map[*types.TypeName]string

// funcID is the ID of a function or method. Plain functions use FullName.
// Methods use "(T).name" or "(*T).name", where T is the declaring type's
// typeID: no constraints, the declaration's param names, aliases resolved.
// Generic instances map to their declaration.
func (ids idTable) funcID(fn *types.Func) string {
	fn = fn.Origin()
	named, pointer := receiver(fn)
	if named == nil {
		return fn.FullName()
	}
	star := ""
	if pointer {
		star = "*"
	}
	return "(" + star + ids.typeID(named.Obj()) + ")." + fn.Name()
}

// receiverOwner is the ID of the type a method is declared on, or "" for a
// plain function. *Store and aliases of Store both give Store's ID.
func (ids idTable) receiverOwner(fn *types.Func) string {
	if named, _ := receiver(fn); named != nil {
		return ids.typeID(named.Obj())
	}
	return ""
}

// receiver is the named type fn is a method of, and whether the receiver is a
// pointer. It's nil for plain functions (or code with type errors).
func receiver(fn *types.Func) (named *types.Named, pointer bool) {
	recv := fn.Signature().Recv()
	if recv == nil {
		return nil, false
	}
	typ := recv.Type()
	if p, ok := typ.(*types.Pointer); ok {
		typ, pointer = p.Elem(), true
	}
	named, _ = types.Unalias(typ).(*types.Named)
	return named, pointer
}

// typeID is "pkg.Name", with type param names for generics: "pkg.List[T]".
// (types.TypeString would give "pkg.List[T any]", which method IDs don't use.)
// Local types use the ID the walk assigned: "parent.name#n".
func (ids idTable) typeID(tn *types.TypeName) string {
	if tn.Pkg() == nil {
		return tn.Name() // built-in, e.g. error
	}
	id := tn.Pkg().Path() + "." + tn.Name()
	if tn.Parent() != tn.Pkg().Scope() {
		if local, ok := ids[tn]; ok {
			return local
		}
		return id + "#?" // a local type the walk missed: never a package-level ID
	}
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

// VarID is "pkg.name" for a package-level var or const.
func VarID(obj types.Object) string {
	return obj.Pkg().Path() + "." + obj.Name()
}

// FieldID is the owning type's ID plus the field name: "pkg.Store.users".
func FieldID(ownerID, name string) string {
	return ownerID + "." + name
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
