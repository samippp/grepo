// Types: the Type nodes, and the implementations computed from them after the walk.

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

	// Kept for implementations(). Generics can't be checked without type args.
	if !isGeneric(tn) {
		if node.Kind == InterfaceType {
			b.interfaces = append(b.interfaces, tn)
		} else {
			b.concrete = append(b.concrete, tn)
		}
	}
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

// implementations records which concrete repo types satisfy which repo
// interfaces, across all packages. Each type is only checked against
// interfaces indexed under one of its methods.
func implementations(concrete, interfaces []*types.TypeName, ids idTable) []Implementation {
	out := []Implementation{}
	byMethod := indexInterfaces(interfaces)
	for _, tn := range concrete {
		for _, entry := range candidateInterfaces(tn, byMethod) {
			value := types.Implements(tn.Type(), entry.iface)
			if !value && !types.Implements(types.NewPointer(tn.Type()), entry.iface) {
				continue
			}
			out = append(out, Implementation{
				Type:        ids.typeID(tn),
				Interface:   ids.typeID(entry.name),
				PointerOnly: !value, // *T always has T's methods, so only the reverse can fail
			})
		}
	}
	return out
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
