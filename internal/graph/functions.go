// Functions: the Function nodes (declared functions and methods, interface
// methods, closures) and Func, the function a value is.

package graph

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
)

// handleFuncDecl records a declared function or method, then walks its body.
func (b *callGraphBuilder) handleFuncDecl(funcDecl *ast.FuncDecl) {
	// Missing only if the package has type errors; skip it.
	fn, ok := b.pkg.TypesInfo.Defs[funcDecl.Name].(*types.Func)
	if !ok {
		return
	}

	id := b.assignFuncDeclID(funcDecl, fn)
	b.functions = append(b.functions, b.newFunction(fn, id))
	b.expr(id, funcDecl.Type) // consts in its signature: func f(b [Max]byte)

	// A nil *BlockStmt passed as ast.Node isn't == nil, so check here.
	if funcDecl.Body != nil {
		b.numberClosures(id, id, funcDecl.Body)
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

// addInterfaceMethod records a method declared in an interface as a
// Function owned by that interface. It has no body, so no calls.
func (b *callGraphBuilder) addInterfaceMethod(fn *types.Func) {
	node := b.newFunction(fn, b.ids.funcID(fn))
	node.Receiver = nil // interface methods have no written receiver
	b.functions = append(b.functions, node)
}

// Closures. See docs/architecture.md for the rules.

// ClosureID is grepo's ID for a func literal in this graph, or "" if it wasn't walked.
func (g *CallGraph) ClosureID(lit *ast.FuncLit) string { return g.closures[lit] }

// numberClosures gives each func literal under n the ID prefix$k, in source
// order, with parent as its Parent, and numbers nested ones under their own ID.
// Numbering is a pass of its own because the walk doesn't visit in source order.
func (b *callGraphBuilder) numberClosures(prefix, parent string, n ast.Node) {
	ast.Inspect(n, func(c ast.Node) bool {
		lit, ok := c.(*ast.FuncLit)
		if !ok {
			return true
		}
		b.closureCount[prefix]++
		id := fmt.Sprintf("%s$%d", prefix, b.closureCount[prefix])
		b.closures[lit] = id
		b.closureParent[lit] = parent
		b.numberClosures(id, id, lit.Body)
		return false // the pass above numbered what's inside
	})
}

// addClosure records lit's Function node and returns its ID.
func (b *callGraphBuilder) addClosure(lit *ast.FuncLit) string {
	id := b.closures[lit]
	node := Function{
		ID:      id,
		Name:    id[strings.LastIndex(id, ".")+1:], // main$1
		Package: b.pkg.PkgPath,
		Pos:     b.pos(lit.Pos()),
		Parent:  b.closureParent[lit],
	}
	if sig, ok := b.pkg.TypesInfo.TypeOf(lit).(*types.Signature); ok {
		node.Params, node.Results = tupleParams(sig.Params()), tupleParams(sig.Results())
	}
	b.functions = append(b.functions, node)
	return id
}

// funcValue is the ID of the function e is: a closure, a declared function or
// generic instance, or a method value or expression; "" for anything else,
// including locals and fields holding functions.
func (b *callGraphBuilder) funcValue(e ast.Expr) string {
	info := b.pkg.TypesInfo
	switch x := unindex(ast.Unparen(e)).(type) {
	case *ast.FuncLit:
		return b.closures[x]
	case *ast.Ident:
		if fn, ok := info.Uses[x].(*types.Func); ok {
			return b.ids.funcID(fn)
		}
	case *ast.SelectorExpr:
		if sel := info.Selections[x]; sel != nil {
			if sel.Kind() != types.FieldVal {
				return b.ids.funcID(sel.Obj().(*types.Func)) // s.handle, (*T).M
			}
		} else if fn, ok := info.Uses[x.Sel].(*types.Func); ok {
			return b.ids.funcID(fn) // pkg.Func
		}
	}
	return ""
}

// numberValueClosures numbers the closures in a package-level spec's values
// under the var each initializes: value i goes with name i, a single value with
// the first name. A blank var has no node, so its closures are the package's.
func (b *callGraphBuilder) numberValueClosures(spec *ast.ValueSpec) {
	for i, value := range spec.Values {
		name := spec.Names[0]
		if len(spec.Values) == len(spec.Names) {
			name = spec.Names[i]
		}
		prefix, parent := b.pkg.PkgPath+"."+name.Name, b.pkg.PkgPath+"."+name.Name
		if name.Name == "_" {
			parent = b.pkg.PkgPath
		}
		b.numberClosures(prefix, parent, value)
	}
}
