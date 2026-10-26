// Variables: the Variable nodes for package-level vars and consts.

package graph

import (
	"go/ast"
	"go/types"
)

// handleValueSpec records each var or const in spec, plus the calls and
// accesses in its values, which run at package startup so their caller is the package (#7).
func (b *callGraphBuilder) handleValueSpec(spec *ast.ValueSpec) {
	b.numberValueClosures(spec)
	for i, name := range spec.Names {
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
			edge := Access{Accessor: b.pkg.PkgPath, Target: node.ID, Pos: b.pos(name.Pos()), Kind: ConstructAccess} // its initial value
			if len(spec.Values) == len(spec.Names) {
				edge.Func = b.funcValue(spec.Values[i])
			}
			b.accesses = append(b.accesses, edge)
		}
		b.variables = append(b.variables, node)
	}

	b.expr(b.pkg.PkgPath, spec.Type) // consts in it: var t [Max]int
	for _, value := range spec.Values {
		b.handed(b.pkg.PkgPath, value)
	}
}
