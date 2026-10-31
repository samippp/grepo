// Types: the Type nodes. Implementations are matched from their summaries (implementations.go).

package graph

import (
	"fmt"
	"go/ast"
	"go/types"
)

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

	b.summarize(tn)
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

// isGeneric reports whether tn has type parameters, like List[T].
func isGeneric(tn *types.TypeName) bool {
	named, ok := tn.Type().(*types.Named)
	return ok && named.TypeParams().Len() > 0
}
