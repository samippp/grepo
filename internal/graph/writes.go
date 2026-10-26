// Writes: the Access edges. See docs/architecture.md for the rules.
// Each handler records its writes and hands what it doesn't write to expr.

package graph

import (
	"go/ast"
	"go/token"
	"go/types"
)

// addAssign records each target of an = or op= assignment, then walks the
// right side. := only declares locals, so it writes nothing.
func (b *callGraphBuilder) addAssign(accessor string, assign *ast.AssignStmt) {
	if assign.Tok != token.DEFINE {
		for i, lhs := range assign.Lhs {
			kind, match := b.assignKind(assign, i)
			if match != nil {
				b.suppressed[match] = true // update only: no read of the matching expression
			}
			fn := ""
			if kind == AssignAccess && len(assign.Rhs) == len(assign.Lhs) {
				fn = b.funcValue(assign.Rhs[i]) // s.onErr = h
			}
			b.addWrite(accessor, lhs, kind, fn)
		}
	}
	for _, rhs := range assign.Rhs {
		b.handed(accessor, rhs)
	}
}

// assignKind is how assign changes its i-th target: x += v and x = f(x) update
// it. match is the right-side expression that makes it an update, if any.
func (b *callGraphBuilder) assignKind(assign *ast.AssignStmt, i int) (kind AccessKind, match ast.Expr) {
	if assign.Tok != token.ASSIGN {
		return UpdateAccess, nil
	}
	rhs := assign.Rhs[0] // a, b = f(): one value for all
	if len(assign.Rhs) == len(assign.Lhs) {
		rhs = assign.Rhs[i]
	}
	if m := b.matching(rhs, assign.Lhs[i]); m != nil {
		return UpdateAccess, m
	}
	return AssignAccess, nil
}

// addBuiltinWrite records the write delete, clear and copy make into their
// first argument, and reports whether it did.
func (b *callGraphBuilder) addBuiltinWrite(accessor string, call *ast.CallExpr) bool {
	name, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || len(call.Args) == 0 {
		return false
	}
	if builtin, ok := b.pkg.TypesInfo.Uses[name].(*types.Builtin); ok {
		switch builtin.Name() {
		case "delete", "clear", "copy":
			b.addContentWrite(accessor, call.Args[0], call.Pos())
			return true
		}
	}
	return false
}

// addConstruct records each field a struct literal sets: T{f: v}, T{v1, v2}.
func (b *callGraphBuilder) addConstruct(accessor string, lit *ast.CompositeLit) {
	named, st := b.literalStruct(lit)
	if st == nil || named == nil {
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
			value := elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				value = kv.Value
			}
			b.accesses = append(b.accesses, Access{Accessor: accessor, Target: FieldID(owner, field.Name()), Pos: b.pos(elt.Pos()), Kind: ConstructAccess, Func: b.funcValue(value)})
		}
	}
}

// literalStruct is the struct type lit builds, and its named type if it has one.
// st is nil for other literals.
func (b *callGraphBuilder) literalStruct(lit *ast.CompositeLit) (named *types.Named, st *types.Struct) {
	typ := b.pkg.TypesInfo.TypeOf(lit)
	if typ == nil {
		return nil, nil
	}
	if p, ok := typ.(*types.Pointer); ok {
		typ = p.Elem() // an elided &T{...}
	}
	named, _ = types.Unalias(typ).(*types.Named)
	st, _ = typ.Underlying().(*types.Struct)
	return named, st
}

// addWrite records a write of kind to e, then content writes outward: to every
// struct value whose memory holds e (s.inner.x = 1 changes s.inner), and to the
// field holding the first pointer on the way, where the walk stops. fn is the
// function the written value is, for e's own edge.
func (b *callGraphBuilder) addWrite(accessor string, e ast.Expr, kind AccessKind, fn string) {
	if e == nil {
		return
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.IndexExpr:
		b.addContentWrite(accessor, x.X, x.Pos()) // the index decides: x[k]++ is content
		b.expr(accessor, x.Index)
		return
	case *ast.StarExpr:
		b.addContentWrite(accessor, x.X, x.Pos())
		return
	case *ast.SelectorExpr:
		sel := b.pkg.TypesInfo.Selections[x]
		if sel == nil || sel.Kind() != types.FieldVal {
			break // pkg.Var
		}
		steps, _ := b.fieldPath(sel.Recv(), sel.Index())
		last := len(steps) - 1
		pos := b.pos(x.Pos())
		if id := steps[last].id; id != "" {
			b.accesses = append(b.accesses, Access{Accessor: accessor, Target: id, Pos: pos, Kind: kind, Func: fn})
		}
		// Out through promoted fields (t.Name via embedded User), then to x.X.
		for i := last; i > 0; i-- {
			if id := steps[i-1].id; id != "" {
				b.accesses = append(b.accesses, Access{Accessor: accessor, Target: id, Pos: pos, Kind: ContentAccess})
			}
			if steps[i].viaPointer {
				// steps[i-1] holds a pointer: what it points to changed, not it.
				b.addReads(accessor, steps[:i-1], pos)
				b.expr(accessor, x.X)
				return
			}
		}
		if steps[0].viaPointer {
			b.addContentWrite(accessor, x.X, x.Pos()) // p.x = v: like (*p).x = v
		} else {
			b.addWrite(accessor, x.X, ContentAccess, "")
		}
		return
	}
	// A package var, by name or pkg.Var. Locals have no holder: no edge.
	if id := b.holderID(e); id != "" {
		b.accesses = append(b.accesses, Access{Accessor: accessor, Target: id, Pos: b.pos(e.Pos()), Kind: kind, Func: fn})
	} else {
		b.expr(accessor, e)
	}
}

// addContentWrite records a write into what e holds: e[k] = v, *e = v,
// delete(e, k). An array's elements are its own memory, so that's a write
// to e itself; a slice, map or pointer's are shared, so only e is recorded.
func (b *callGraphBuilder) addContentWrite(accessor string, e ast.Expr, pos token.Pos) {
	if _, ok := b.pkg.TypesInfo.TypeOf(e).Underlying().(*types.Array); ok {
		b.addWrite(accessor, e, ContentAccess, "")
		return
	}
	b.addHolderContent(accessor, e, pos)
}

// addHolderContent records content on the var or field holderID(e) finds; the
// rest of e (indexes, the path to the holder) goes to expr.
func (b *callGraphBuilder) addHolderContent(accessor string, e ast.Expr, pos token.Pos) {
	switch x := ast.Unparen(e).(type) {
	case *ast.IndexExpr:
		b.expr(accessor, x.Index)
		b.addHolderContent(accessor, x.X, pos)
		return
	case *ast.SliceExpr:
		b.expr(accessor, x.Low)
		b.expr(accessor, x.High)
		b.expr(accessor, x.Max)
		b.addHolderContent(accessor, x.X, pos)
		return
	case *ast.StarExpr:
		b.addHolderContent(accessor, x.X, pos)
		return
	case *ast.SelectorExpr:
		sel := b.pkg.TypesInfo.Selections[x]
		if sel == nil || sel.Kind() != types.FieldVal {
			break
		}
		steps, _ := b.fieldPath(sel.Recv(), sel.Index())
		last := len(steps) - 1
		if id := steps[last].id; id != "" {
			b.accesses = append(b.accesses, Access{Accessor: accessor, Target: id, Pos: b.pos(pos), Kind: ContentAccess})
		}
		b.addReads(accessor, steps[:last], b.pos(x.Pos()))
		b.expr(accessor, x.X)
		return
	}
	if id := b.holderID(e); id != "" {
		b.accesses = append(b.accesses, Access{Accessor: accessor, Target: id, Pos: b.pos(pos), Kind: ContentAccess})
	} else {
		b.expr(accessor, e)
	}
}

// holderID is the ID of the var or field e names, looking through
// indexes and derefs (s.m[a][b] → s.m), or "" for locals and expressions.
func (b *callGraphBuilder) holderID(e ast.Expr) string {
	switch x := ast.Unparen(e).(type) {
	case *ast.IndexExpr:
		return b.holderID(x.X)
	case *ast.SliceExpr:
		return b.holderID(x.X)
	case *ast.StarExpr:
		return b.holderID(x.X)
	}
	return b.targetID(e)
}

// targetID is the ID of the package var or field e is, or "" for anything else.
func (b *callGraphBuilder) targetID(e ast.Expr) string {
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
			steps, _ := b.fieldPath(sel.Recv(), sel.Index())
			return steps[len(steps)-1].id
		}
	}
	return ""
}

// matching returns the expression in rhs that is lhs: the same objects, field
// by field, or nil. Field IDs are per type, so s.n = other.n + 1 doesn't count.
func (b *callGraphBuilder) matching(rhs, lhs ast.Expr) ast.Expr {
	var found ast.Expr
	ast.Inspect(rhs, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && found == nil && b.sameExpr(e, lhs) {
			found = e
		}
		return found == nil
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

// fieldPath lists the fields index goes through from recv, outermost first, and
// the last one's type: t.Name with an embedded User is [Tagged.User, User.Name].
func (b *callGraphBuilder) fieldPath(recv types.Type, index []int) ([]fieldStep, types.Type) {
	var steps []fieldStep
	typ := recv
	for _, i := range index {
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
	return steps, typ
}

// packageVar returns obj if it's a package-level var, in the repo or not.
func packageVar(obj types.Object) *types.Var {
	v, ok := obj.(*types.Var)
	if !ok || v.Pkg() == nil || v.Parent() != v.Pkg().Scope() {
		return nil
	}
	return v
}
