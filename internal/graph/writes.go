// Writes: the Access edges. See docs/architecture.md for the rules.

package graph

import (
	"go/ast"
	"go/token"
	"go/types"
)

// addAssign records each target of an = or op= assignment. := only declares
// locals, so it writes nothing.
func (b *callGraphBuilder) addAssign(accessor string, assign *ast.AssignStmt) {
	if assign.Tok == token.DEFINE {
		return
	}
	for i, lhs := range assign.Lhs {
		kind := UpdateAccess // x += v
		if assign.Tok == token.ASSIGN {
			rhs := assign.Rhs[0] // a, b = f(): one value for all
			if len(assign.Rhs) == len(assign.Lhs) {
				rhs = assign.Rhs[i]
			}
			kind = AssignAccess
			if b.mentions(rhs, lhs) {
				kind = UpdateAccess // x = f(x)
			}
		}
		b.addWrite(accessor, lhs, kind)
	}
}

// addBuiltinWrite records the write delete, clear and copy make into their
// first argument.
func (b *callGraphBuilder) addBuiltinWrite(accessor string, call *ast.CallExpr) {
	name, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || len(call.Args) == 0 {
		return
	}
	if builtin, ok := b.pkg.TypesInfo.Uses[name].(*types.Builtin); ok {
		switch builtin.Name() {
		case "delete", "clear", "copy":
			b.addContentWrite(accessor, call.Args[0], call.Pos())
		}
	}
}

// addConstruct records each field a struct literal sets: T{f: v}, T{v1, v2}.
func (b *callGraphBuilder) addConstruct(accessor string, lit *ast.CompositeLit) {
	typ := b.pkg.TypesInfo.TypeOf(lit)
	if p, ok := typ.(*types.Pointer); ok {
		typ = p.Elem() // an elided &T{...}
	}
	named, _ := types.Unalias(typ).(*types.Named)
	st, ok := typ.Underlying().(*types.Struct)
	if !ok || named == nil {
		return // not a struct, or an unnamed struct type: no field IDs
	}
	owner := b.ids.typeID(named.Origin().Obj()) // List[int]{...} → List[T]
	for i, elt := range lit.Elts {
		var field *types.Var
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			field, _ = b.pkg.TypesInfo.Uses[kv.Key.(*ast.Ident)].(*types.Var)
		} else if i < st.NumFields() {
			field = st.Field(i) // positional
		}
		if field != nil {
			b.accesses = append(b.accesses, Access{Accessor: accessor, Target: FieldID(owner, field.Name()), Pos: b.pos(elt.Pos()), Kind: ConstructAccess})
		}
	}
}

// addWrite records a write of kind to e, then a content write to every
// struct value whose memory holds e: s.inner.x = 1 also changes s.inner.
func (b *callGraphBuilder) addWrite(accessor string, e ast.Expr, kind AccessKind) {
	if e == nil {
		return
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.IndexExpr:
		b.addContentWrite(accessor, x.X, x.Pos()) // the index decides: x[k]++ is content
	case *ast.SliceExpr:
		b.addContentWrite(accessor, x.X, x.Pos())
	case *ast.StarExpr:
		b.addContentWrite(accessor, x.X, x.Pos())
	case *ast.Ident:
		if v := packageVar(b.pkg.TypesInfo.Uses[x]); v != nil {
			b.accesses = append(b.accesses, Access{Accessor: accessor, Target: VarID(v), Pos: b.pos(x.Pos()), Kind: kind})
		}
		// Locals: no edge.
	case *ast.SelectorExpr:
		sel := b.pkg.TypesInfo.Selections[x]
		if sel == nil { // pkg.Var
			if v := packageVar(b.pkg.TypesInfo.Uses[x.Sel]); v != nil {
				b.accesses = append(b.accesses, Access{Accessor: accessor, Target: VarID(v), Pos: b.pos(x.Pos()), Kind: kind})
			}
			return
		}
		if sel.Kind() != types.FieldVal {
			return
		}
		steps := b.fieldPath(sel)
		last := len(steps) - 1
		pos := b.pos(x.Pos())
		if id := steps[last].id; id != "" {
			b.accesses = append(b.accesses, Access{Accessor: accessor, Target: id, Pos: pos, Kind: kind})
		}
		// Out through promoted fields (t.Name via embedded User), then to x.X.
		for i := last; i > 0; i-- {
			if steps[i].viaPointer {
				return
			}
			if id := steps[i-1].id; id != "" {
				b.accesses = append(b.accesses, Access{Accessor: accessor, Target: id, Pos: pos, Kind: ContentAccess})
			}
		}
		if !steps[0].viaPointer {
			b.addWrite(accessor, x.X, ContentAccess)
		}
	}
}

// addContentWrite records a write into what e holds: e[k] = v, *e = v,
// delete(e, k). An array's elements are its own memory, so that's a write
// to e itself; a slice, map or pointer's are shared, so only e is recorded.
func (b *callGraphBuilder) addContentWrite(accessor string, e ast.Expr, pos token.Pos) {
	if _, ok := b.pkg.TypesInfo.TypeOf(e).Underlying().(*types.Array); ok {
		b.addWrite(accessor, e, ContentAccess)
		return
	}
	if id := b.holderID(e); id != "" {
		b.accesses = append(b.accesses, Access{Accessor: accessor, Target: id, Pos: b.pos(pos), Kind: ContentAccess})
	}
}

// holderID is the ID of the var or field e names, looking through
// indexes and derefs (s.m[a][b] → s.m), or "" for locals and expressions.
func (b *callGraphBuilder) holderID(e ast.Expr) string {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if v := packageVar(b.pkg.TypesInfo.Uses[x]); v != nil {
			return VarID(v)
		}
	case *ast.SelectorExpr:
		sel := b.pkg.TypesInfo.Selections[x]
		if sel == nil {
			if v := packageVar(b.pkg.TypesInfo.Uses[x.Sel]); v != nil {
				return VarID(v)
			}
			return ""
		}
		if sel.Kind() == types.FieldVal {
			steps := b.fieldPath(sel)
			return steps[len(steps)-1].id
		}
	case *ast.IndexExpr:
		return b.holderID(x.X)
	case *ast.SliceExpr:
		return b.holderID(x.X)
	case *ast.StarExpr:
		return b.holderID(x.X)
	}
	return ""
}

// mentions reports whether rhs contains the expression lhs: the same objects,
// field by field. Field IDs are per type, so s.n = other.n + 1 doesn't count.
func (b *callGraphBuilder) mentions(rhs, lhs ast.Expr) bool {
	found := false
	ast.Inspect(rhs, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && b.sameExpr(e, lhs) {
			found = true
		}
		return !found
	})
	return found
}

// sameExpr reports whether x and y name the same var or field path.
func (b *callGraphBuilder) sameExpr(x, y ast.Expr) bool {
	info := b.pkg.TypesInfo
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		y, ok := ast.Unparen(y).(*ast.Ident)
		return ok && info.ObjectOf(x) != nil && info.ObjectOf(x) == info.ObjectOf(y)
	case *ast.SelectorExpr:
		y, ok := ast.Unparen(y).(*ast.SelectorExpr)
		return ok && info.ObjectOf(x.Sel) == info.ObjectOf(y.Sel) && b.sameExpr(x.X, y.X)
	}
	return false
}

// fieldStep is one field on a selector's path.
type fieldStep struct {
	id         string // "" for a field of an unnamed struct type
	viaPointer bool   // its struct was reached through a pointer
}

// fieldPath lists the fields sel goes through, outermost first: t.Name with
// an embedded User is [Tagged.User, User.Name].
func (b *callGraphBuilder) fieldPath(sel *types.Selection) []fieldStep {
	var steps []fieldStep
	typ := sel.Recv()
	for _, i := range sel.Index() {
		step := fieldStep{}
		if p, ok := typ.Underlying().(*types.Pointer); ok {
			typ, step.viaPointer = p.Elem(), true
		}
		field := typ.Underlying().(*types.Struct).Field(i)
		if named, ok := types.Unalias(typ).(*types.Named); ok {
			step.id = FieldID(b.ids.typeID(named.Origin().Obj()), field.Name()) // List[int].items → List[T].items
		}
		steps = append(steps, step)
		typ = field.Type()
	}
	return steps
}

// packageVar returns obj if it's a package-level var, in the repo or not.
func packageVar(obj types.Object) *types.Var {
	v, ok := obj.(*types.Var)
	if !ok || v.Pkg() == nil || v.Parent() != v.Pkg().Scope() {
		return nil
	}
	return v
}
