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
	info := b.pkg.TypesInfo
	if tv := info.Types[call.Fun]; tv.IsType() || tv.IsBuiltin() {
		return "", "", false // int64(n), len(x)
	}

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
	return b.ids.funcID(fn), callKind(fn), true // funcID: List[int].Push → List[T].Push
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
			Func: b.funcValue(arg),
		})
	}
	return args
}
