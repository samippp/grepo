// IDs: every node's ID but closures' (functions.go). See docs/architecture.md, ID rules.

package graph

import (
	"go/types"
	"strings"
)

// idTable holds the IDs the walk assigns: local types' (declared inside a
// function) and init functions'. Every other ID is computed from the object
// alone.
type idTable map[types.Object]string

// funcID is FullName for a function, and "(T).name" or "(*T).name" for a
// method, T being the declaring type's typeID. Generic instances map to
// their declaration. An init function has the ID the walk gave it.
func (ids idTable) funcID(fn *types.Func) string {
	fn = fn.Origin()
	if id, ok := ids[fn]; ok {
		return id
	}
	named, pointer := receiver(fn.Signature())
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
	return ids.receiverTypeID(fn.Signature())
}

// receiverTypeID is the ID of the named type sig's receiver is, or "" if it
// has none.
func (ids idTable) receiverTypeID(sig *types.Signature) string {
	if named, _ := receiver(sig); named != nil {
		return ids.typeID(named.Obj())
	}
	return ""
}

// receiver is the named type sig's receiver is, and whether the receiver is a
// pointer. It's nil for plain functions (or code with type errors).
func receiver(sig *types.Signature) (named *types.Named, pointer bool) {
	recv := sig.Recv()
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

// typeID is "pkg.Name", "pkg.List[T]" for generics (not TypeString's
// "List[T any]"), or the walk's "parent.name#n" for local types.
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

// uses notes the name of pkg, if another package an ID names, for Dependencies.
func (b *callGraphBuilder) uses(pkg *types.Package) {
	if pkg != nil && pkg.Path() != b.pkg.PkgPath {
		b.deps[pkg.Path()] = pkg.Name()
	}
}

// varID is VarID(v), with its package noted (uses).
func (b *callGraphBuilder) varID(v *types.Var) string {
	b.uses(v.Pkg())
	return VarID(v)
}

// VarID is "pkg.name" for a package-level var or const.
func VarID(obj types.Object) string {
	return obj.Pkg().Path() + "." + obj.Name()
}

// FieldID is the owning type's ID plus the field name: "pkg.Store.users".
func FieldID(ownerID, name string) string {
	return ownerID + "." + name
}
