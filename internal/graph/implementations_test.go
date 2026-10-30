package graph

import (
	"encoding/json"
	"fmt"
	"go/types"
	"maps"
	"testing"
)

// Matching from summaries agrees with types.Implements. Summaries go
// through JSON first, as they will through the store.
func TestImplementationsFromSummaries(t *testing.T) {
	res := loadFixture(t)
	repo := map[string]bool{}
	for _, pkg := range res.Packages {
		repo[pkg.PkgPath] = true
	}

	var summaries []typeSummary
	var concrete, interfaces []*types.TypeName
	ids := idTable{}
	for _, pkg := range res.Packages {
		b := buildPackage(pkg, res.Dir, repo)
		summaries = append(summaries, b.summary)
		maps.Copy(ids, b.ids)

		// Every named type the walk visits, package-level and local.
		for _, obj := range pkg.TypesInfo.Defs {
			tn, ok := obj.(*types.TypeName)
			if !ok || tn.IsAlias() || isGeneric(tn) {
				continue
			}
			switch u := tn.Type().Underlying().(type) {
			case *types.TypeParam:
			case *types.Interface:
				// Left out on purpose: types.Implements would match constraints too.
				if u.IsMethodSet() {
					interfaces = append(interfaces, tn)
				}
			default:
				concrete = append(concrete, tn)
			}
		}
	}
	data, err := json.Marshal(summaries)
	if err != nil {
		t.Fatal(err)
	}
	var stored []typeSummary
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}

	want := implementsOracle(concrete, interfaces, ids)
	if len(want) == 0 {
		t.Fatal("the fixture has no implementations")
	}
	checkExact(t, "implementation", matchImplementations(stored), want, func(im Implementation) string { return fmt.Sprintf("%+v", im) })
}

// implementsOracle is the matcher from before summaries: types.Implements on
// every pair of a concrete type and a non-empty interface.
func implementsOracle(concrete, interfaces []*types.TypeName, ids idTable) []Implementation {
	var out []Implementation
	for _, tn := range concrete {
		for _, in := range interfaces {
			iface := in.Type().Underlying().(*types.Interface)
			if iface.NumMethods() == 0 {
				continue
			}
			value := types.Implements(tn.Type(), iface)
			if !value && !types.Implements(types.NewPointer(tn.Type()), iface) {
				continue
			}
			out = append(out, Implementation{Type: ids.typeID(tn), Interface: ids.typeID(in), PointerOnly: !value})
		}
	}
	return out
}
