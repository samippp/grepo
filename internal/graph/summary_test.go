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
		concrete = append(concrete, b.concrete...)
		interfaces = append(interfaces, b.interfaces...)
		maps.Copy(ids, b.ids)
	}
	data, err := json.Marshal(summaries)
	if err != nil {
		t.Fatal(err)
	}
	var stored []typeSummary
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}

	want := implementations(concrete, interfaces, ids)
	if len(want) == 0 {
		t.Fatal("the fixture has no implementations")
	}
	checkExact(t, "implementation", matchImplementations(stored), want, func(im Implementation) string { return fmt.Sprintf("%+v", im) })
}
