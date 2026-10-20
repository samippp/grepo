// Reads and escapes: the read and escape Access edges, and Through on calls.
// See docs/architecture.md for the rules.

package graph

import (
	"go/ast"
	"go/token"
	"go/types"
)

// addValueRead records a read of obj if it's a package var or a repo const.
func (b *callGraphBuilder) addValueRead(accessor string, obj types.Object, pos token.Pos) {
	var target string
	switch obj := obj.(type) {
	case *types.Var:
		if packageVar(obj) != nil {
			target = VarID(obj)
		}
	case *types.Const:
		// Outside consts can't change and no consumer needs them; local consts have no node.
		if obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() && b.repo[obj.Pkg().Path()] {
			target = VarID(obj)
		}
	}
	if target != "" {
		b.accesses = append(b.accesses, Access{Accessor: accessor, Target: target, Pos: b.pos(pos), Kind: ReadAccess})
	}
}

// addSelector records x.Sel as a value: a field read (every hop), a
// qualified var or const, or a method's receiver.
func (b *callGraphBuilder) addSelector(accessor string, x *ast.SelectorExpr) {
	sel := b.pkg.TypesInfo.Selections[x]
	if sel == nil {
		b.addValueRead(accessor, b.pkg.TypesInfo.Uses[x.Sel], x.Pos()) // pkg.Name
		return
	}
	switch sel.Kind() {
	case types.FieldVal:
		steps, _ := b.fieldPath(sel.Recv(), sel.Index())
		b.addReads(accessor, steps, b.pos(x.Pos()))
		b.expr(accessor, x.X)
	case types.MethodVal:
		b.addReceiver(accessor, x, sel)
	}
	// MethodExpr ((*T).M): x.X is a type.
}

// addReceiver records the receiver of a method call or method value: an
// escape where Go takes its address (pointer method on a value), else a read.
// Promoted methods go through embedded fields, which count as hops.
func (b *callGraphBuilder) addReceiver(accessor string, x *ast.SelectorExpr, sel *types.Selection) {
	index := sel.Index()
	steps, typ := b.fieldPath(sel.Recv(), index[:len(index)-1])
	if len(steps) == 0 {
		typ = b.pkg.TypesInfo.TypeOf(x.X)
	}
	pos := b.pos(x.Pos())
	if takesAddress(sel.Obj().(*types.Func), typ) {
		if len(steps) > 0 {
			b.addAddressPath(accessor, steps, x.X, pos, true)
		} else {
			b.addAddress(accessor, x.X, true)
		}
		return
	}
	b.addReads(accessor, steps, pos)
	b.expr(accessor, x.X)
}

// takesAddress reports whether calling fn on a value of type recv makes Go
// take its address: a pointer method on a non-pointer. Interface methods never do.
func takesAddress(fn *types.Func, recv types.Type) bool {
	if _, pointer := receiver(fn); !pointer || recv == nil {
		return false
	}
	_, isPointer := recv.Underlying().(*types.Pointer)
	return !isPointer
}

// addAddress records &e: an escape of the field or var e is (if escape), and
// no reads on struct-value hops up to the first pointer.
func (b *callGraphBuilder) addAddress(accessor string, e ast.Expr, escape bool) {
	if x, ok := ast.Unparen(e).(*ast.SelectorExpr); ok {
		if sel := b.pkg.TypesInfo.Selections[x]; sel != nil && sel.Kind() == types.FieldVal {
			steps, _ := b.fieldPath(sel.Recv(), sel.Index())
			b.addAddressPath(accessor, steps, x.X, b.pos(x.Pos()), escape)
			return
		}
	}
	if target := b.targetID(e); target != "" {
		if escape {
			b.accesses = append(b.accesses, Access{Accessor: accessor, Target: target, Pos: b.pos(e.Pos()), Kind: EscapeAccess})
		}
		return
	}
	b.expr(accessor, e) // a local, &T{...}, &s.items[i], …
}

// addAddressPath is addAddress for base.steps: the first pointer met going
// outward is loaded, so it and everything before it are reads.
func (b *callGraphBuilder) addAddressPath(accessor string, steps []fieldStep, base ast.Expr, pos string, escape bool) {
	last := len(steps) - 1
	if escape && steps[last].id != "" {
		b.accesses = append(b.accesses, Access{Accessor: accessor, Target: steps[last].id, Pos: pos, Kind: EscapeAccess})
	}
	for i := last; i > 0; i-- {
		if steps[i].viaPointer {
			b.addReads(accessor, steps[:i], pos)
			b.expr(accessor, base)
			return
		}
	}
	if steps[0].viaPointer {
		b.expr(accessor, base)
	} else {
		b.addAddress(accessor, base, false)
	}
}

// addReads records a read of each step.
func (b *callGraphBuilder) addReads(accessor string, steps []fieldStep, pos string) {
	for _, step := range steps {
		if step.id != "" {
			b.accesses = append(b.accesses, Access{Accessor: accessor, Target: step.id, Pos: pos, Kind: ReadAccess})
		}
	}
}

// handed walks e, a value that is passed, stored, returned or sent. If e is
// itself a slice or map field or var, its contents escape too.
func (b *callGraphBuilder) handed(accessor string, e ast.Expr) {
	if b.suppressed[e] {
		b.expr(accessor, e)
		return
	}
	b.expr(accessor, e)
	if target := b.targetID(e); target != "" && sharesContents(b.pkg.TypesInfo.TypeOf(e)) {
		b.accesses = append(b.accesses, Access{Accessor: accessor, Target: target, Pos: b.pos(e.Pos()), Kind: EscapeAccess})
	}
}

// sharesContents reports whether a copy of a typ value shares its contents:
// slices and maps. Pointers are left out on purpose.
func sharesContents(typ types.Type) bool {
	if typ == nil {
		return false
	}
	switch typ.Underlying().(type) {
	case *types.Slice, *types.Map:
		return true
	}
	return false
}

// passesArgs reports whether call hands its args to a function: not a
// builtin or a conversion.
func (b *callGraphBuilder) passesArgs(call *ast.CallExpr) bool {
	tv := b.pkg.TypesInfo.Types[call.Fun]
	return !tv.IsType() && !tv.IsBuiltin()
}

// through is the field or package var a call with this fun is made on
// (s.mu.Lock(), the embedded field for promoted methods) or takes its func
// from (s.onError(err)), or "".
func (b *callGraphBuilder) through(fun ast.Expr) string {
	fun = ast.Unparen(fun)
	if x, ok := fun.(*ast.SelectorExpr); ok {
		if sel := b.pkg.TypesInfo.Selections[x]; sel != nil && sel.Kind() == types.MethodVal {
			index := sel.Index()
			if steps, _ := b.fieldPath(sel.Recv(), index[:len(index)-1]); len(steps) > 0 {
				return steps[len(steps)-1].id
			}
			return b.targetID(x.X)
		}
	}
	return b.targetID(fun)
}
