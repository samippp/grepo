// Type summaries: what implementation matching needs from a package, stored
// with it so a cached package needs no type-checking.

package graph

import (
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// typeSummary is one package's types by method set. Maps go from method key
// (unexported names qualified by package) to signature key; equal keys mean
// identical methods. No generic types, empty interfaces or constraints.
type typeSummary struct {
	Concrete   []concreteSummary `json:"concrete"`
	Interfaces []ifaceSummary    `json:"interfaces"`
}

type concreteSummary struct {
	ID             string            `json:"id"`
	ValueMethods   map[string]string `json:"valueMethods"`   // T's
	PointerMethods map[string]string `json:"pointerMethods"` // *T's, a superset
}

type ifaceSummary struct {
	ID      string            `json:"id"`
	Methods map[string]string `json:"methods"` // embedded ones included
}

// summarize adds tn to the package's summary. Generic types can't be checked
// without type args; every type implements an empty interface; interfaces
// with type terms only constrain type params.
func (b *callGraphBuilder) summarize(tn *types.TypeName) {
	if isGeneric(tn) {
		return
	}
	t := tn.Type()
	if iface, ok := t.Underlying().(*types.Interface); ok {
		if iface.NumMethods() > 0 && iface.IsMethodSet() {
			b.summary.Interfaces = append(b.summary.Interfaces, ifaceSummary{ID: b.ids.typeID(tn), Methods: b.ifaceMethods(iface)})
		}
		return
	}
	b.summary.Concrete = append(b.summary.Concrete, concreteSummary{
		ID:             b.ids.typeID(tn),
		ValueMethods:   b.methodSet(t),
		PointerMethods: b.methodSet(types.NewPointer(t)),
	})
}

// methodSet is t's methods, promoted ones included.
func (b *callGraphBuilder) methodSet(t types.Type) map[string]string {
	ms := map[string]string{}
	for sel := range types.NewMethodSet(t).Methods() {
		ms[methodKey(sel.Obj())] = b.typeKey(sel.Type())
	}
	return ms
}

func (b *callGraphBuilder) ifaceMethods(iface *types.Interface) map[string]string {
	ms := map[string]string{}
	for fn := range iface.Methods() {
		ms[methodKey(fn)] = b.typeKey(fn.Type())
	}
	return ms
}

// methodKey is the name, qualified by package when unexported: model's seal
// isn't store's.
func methodKey(obj types.Object) string {
	if obj.Exported() || obj.Pkg() == nil {
		return obj.Name()
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

// typeKey spells t so that equal keys mean identical types: no param names,
// aliases resolved, byte as uint8, named types by grepo ID (two local results
// differ). Not for display.
func (b *callGraphBuilder) typeKey(t types.Type) string {
	switch t := t.(type) {
	case *types.Alias:
		return b.typeKey(types.Unalias(t))
	case *types.Basic:
		return types.Typ[t.Kind()].Name() // byte → uint8, rune → int32
	case *types.Named:
		key := b.ids.typeID(t.Origin().Obj())
		if args := t.TypeArgs(); args.Len() > 0 {
			var keys []string
			for arg := range args.Types() {
				keys = append(keys, b.typeKey(arg))
			}
			key += "[" + strings.Join(keys, ",") + "]"
		}
		return key
	case *types.Pointer:
		return "*(" + b.typeKey(t.Elem()) + ")"
	case *types.Slice:
		return "[](" + b.typeKey(t.Elem()) + ")"
	case *types.Array:
		return "[" + strconv.FormatInt(t.Len(), 10) + "](" + b.typeKey(t.Elem()) + ")"
	case *types.Map:
		return "map[" + b.typeKey(t.Key()) + "](" + b.typeKey(t.Elem()) + ")"
	case *types.Chan:
		dir := map[types.ChanDir]string{types.SendRecv: "chan", types.SendOnly: "chan<-", types.RecvOnly: "<-chan"}[t.Dir()]
		return dir + "(" + b.typeKey(t.Elem()) + ")"
	case *types.Signature:
		params := b.tupleKeys(t.Params())
		if t.Variadic() {
			last := t.Params().At(t.Params().Len() - 1).Type().(*types.Slice)
			params[len(params)-1] = "...(" + b.typeKey(last.Elem()) + ")"
		}
		return "func(" + strings.Join(params, ",") + ")(" + strings.Join(b.tupleKeys(t.Results()), ",") + ")"
	case *types.Struct:
		var fields []string
		for i := range t.NumFields() {
			field, embedded := t.Field(i), ""
			if field.Embedded() {
				embedded = "embedded "
			}
			fields = append(fields, embedded+methodKey(field)+" "+b.typeKey(field.Type())+" "+strconv.Quote(t.Tag(i)))
		}
		return "struct{" + strings.Join(fields, ";") + "}"
	case *types.Interface:
		ms := b.ifaceMethods(t)
		var methods []string
		for _, name := range slices.Sorted(maps.Keys(ms)) {
			methods = append(methods, name+" "+ms[name])
		}
		return "interface{" + strings.Join(methods, ";") + "}"
	}
	return types.TypeString(t, nil) // type params and unions: not in a method of a non-generic type
}

func (b *callGraphBuilder) tupleKeys(tuple *types.Tuple) []string {
	keys := []string{}
	for v := range tuple.Variables() {
		keys = append(keys, b.typeKey(v.Type()))
	}
	return keys
}

// matchImplementations finds implementations across all summaries, as
// implementations() does from type-checker objects. Each interface is indexed
// under its first method key: a type without that method can't implement it.
func matchImplementations(summaries []typeSummary) []Implementation {
	byMethod := map[string][]ifaceSummary{}
	for _, s := range summaries {
		for _, iface := range s.Interfaces {
			first := slices.Min(slices.Collect(maps.Keys(iface.Methods)))
			byMethod[first] = append(byMethod[first], iface)
		}
	}

	out := []Implementation{}
	for _, s := range summaries {
		for _, c := range s.Concrete {
			for name := range c.PointerMethods {
				for _, iface := range byMethod[name] {
					if !hasMethods(c.PointerMethods, iface.Methods) {
						continue
					}
					out = append(out, Implementation{
						Type:        c.ID,
						Interface:   iface.ID,
						PointerOnly: !hasMethods(c.ValueMethods, iface.Methods), // *T has T's methods, so only this can fail
					})
				}
			}
		}
	}
	return out
}

// hasMethods reports whether set has every method in want, with the same signature.
func hasMethods(set, want map[string]string) bool {
	for name, sig := range want {
		if got, ok := set[name]; !ok || got != sig {
			return false
		}
	}
	return true
}
