// Calls: the FunctionCall edges. Through is in reads.go.

package graph

import (
	"go/ast"
	"go/types"
)

// addCall records one call made by callerID.
func (b *callGraphBuilder) addCall(callerID string, call *ast.CallExpr) {
	callee, kind, isCall := b.callee(call)
	if !isCall {
		return
	}
	b.calls = append(b.calls, FunctionCall{
		Caller:   callerID,
		Callee:   callee,
		Pos:      b.pos(call.Pos()),
		CallKind: kind,
		Args:     b.args(call.Args),
		Through:  b.through(call.Fun),
		Stmt:     b.stmt[call],
	})
}

// callee returns the ID of the function call refers to and the call's kind:
// "" and dynamic if unknown. isCall is false for conversions and built-ins.
func (b *callGraphBuilder) callee(call *ast.CallExpr) (id string, kind CallKind, isCall bool) {
	if !b.isCall(call) {
		return "", "", false // int64(n), len(x)
	}
	info := b.pkg.TypesInfo

	var name *ast.Ident
	switch fun := unindex(ast.Unparen(call.Fun)).(type) {
	case *ast.Ident:
		name = fun // foo(), f(), first[int](), handlers[0]()
	case *ast.SelectorExpr:
		name = fun.Sel // pkg.Foo(), obj.Method(), slices.Map[int]()
	case *ast.FuncLit:
		return b.closures[fun], StaticCall, true // func(){...}(): called where it's written
	}
	// Other shapes (f()()) leave name nil: dynamic.

	fn, _ := info.Uses[name].(*types.Func)
	if fn == nil {
		return "", DynamicCall, true
	}
	kind = callKind(fn)
	if kind == InterfaceCall {
		b.calledThrough(fn)
	}
	return b.funcRef(fn), kind, true
}

// calledThrough adds the interface fn is declared on to the package's
// summary, so its implementations are matched even when it's outside the
// repo (io.Writer). Unnamed interfaces have no ID to match.
func (b *callGraphBuilder) calledThrough(fn *types.Func) {
	named, _ := receiver(fn.Origin())
	if named == nil {
		return
	}
	if iface, ok := named.Underlying().(*types.Interface); ok {
		b.summarizeInterface(named.Obj(), iface)
	}
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

// isCall reports whether call calls a function: not a conversion or a built-in.
func (b *callGraphBuilder) isCall(call *ast.CallExpr) bool {
	tv := b.pkg.TypesInfo.Types[call.Fun]
	return !tv.IsType() && !tv.IsBuiltin()
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
			Func: b.funcValue(arg),
		})
	}
	return args
}
