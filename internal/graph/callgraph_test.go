// Expected BuildCallGraph output for testdata/simple: the shared fixture IDs
// and helpers. Each node and edge kind has its own file, like the code.

package graph

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/samippp/grepo/internal/load"
)

const (
	mainPkg  = "example.com/simple"
	storePkg = "example.com/simple/store"
	modelPkg = "example.com/simple/model"

	idMain  = mainPkg + ".main"
	idGreet = mainPkg + ".greet"
	idNew   = storePkg + ".New"
	idGet   = "(*" + storePkg + ".Store).Get"

	idFirst    = mainPkg + ".first"
	idPair     = mainPkg + ".pair"
	idGenerics = mainPkg + ".generics"
	idPush     = "(*" + mainPkg + ".List[T]).Push"
	idUseList  = mainPkg + ".useList"

	idInit1     = mainPkg + ".init@main.go#1"
	idInit2     = mainPkg + ".init@main.go#2"
	idStoreInit = storePkg + ".init@store/store.go#1"
	idListInit  = "(*" + mainPkg + ".List[T]).init"

	// Type IDs. Generic types keep param names but not constraints: List[T], not List[T any].
	tStore     = storePkg + ".Store"
	tGetter    = storePkg + ".Getter"
	tList      = mainPkg + ".List[T]"
	tUser      = modelPkg + ".User"
	tTagged    = modelPkg + ".Tagged"
	tID        = modelPkg + ".ID"
	tUserID    = modelPkg + ".UserID"
	tReadNamer = modelPkg + ".ReadNamer"
	tCache     = storePkg + ".Cache"
	tNamer     = modelPkg + ".Namer"
	tLabel     = modelPkg + ".Label"
	tBadge     = modelPkg + ".Badge"
	tAny       = modelPkg + ".Any"
	tSource    = mainPkg + ".Source[T]"
	tS         = storePkg + ".S"

	// Local types: enclosing function + name + #n, counted per name.
	tResult      = mainPkg + ".result"
	tParseResult = mainPkg + ".parse.result#1"
	tSaveResult1 = mainPkg + ".save.result#1"
	tSaveResult2 = mainPkg + ".save.result#2"
	tLocalNamer  = mainPkg + ".announce.namer#1"
	tJob         = mainPkg + ".spawn$1.job#1"
	tBox         = mainPkg + ".wrap.box#1"

	// Writes fixture (store/writes.go).
	tWInner = storePkg + ".Inner"
	tWBox   = storePkg + ".Box"

	// Reads fixture (store/reads.go).
	tRPart   = storePkg + ".Part"
	tRBase   = storePkg + ".Base"
	tRSub    = storePkg + ".Sub"
	tRHolder = storePkg + ".Holder"
	tRLocked = storePkg + ".Locked"
	tRBag    = storePkg + ".Bag[T]"
	tROuter  = storePkg + ".Outer"

	idValues = "(*" + tRHolder + ").values"
	idCalls  = "(*" + tRHolder + ").calls"
	idHands  = "(*" + tRHolder + ").hands"
	idNested = "(*" + tROuter + ").nested"

	// Closures fixture (store/closures.go).
	tCRunner   = storePkg + ".Runner"
	idClosures = "(*" + tCRunner + ").closures"
	idStart    = "(*" + tCRunner + ").start"

	// Implementations fixture (model/impls.go, store/impls.go).
	tSealed       = modelPkg + ".Sealed"
	tToken        = modelPkg + ".Token"
	tLookup       = modelPkg + ".Lookup"
	tFake         = storePkg + ".Fake"
	tSealer       = storePkg + ".Sealer"
	tFinder       = storePkg + ".Finder"
	tLogger       = storePkg + ".Logger"
	tVarLog       = storePkg + ".varLog"
	tSliceLog     = storePkg + ".sliceLog"
	tMakersResult = storePkg + ".makers.result#1"
	tMaker        = storePkg + ".makers.maker#1"
	tImpl         = storePkg + ".makers.impl#1"
	tOthersResult = storePkg + ".others.result#1"
	tOther        = storePkg + ".others.other#1"
)

// fixtureLoad and fixtureCallGraph load and build testdata/simple once for
// every test. Tests only read them.
var fixtureLoad = sync.OnceValues(func() (*load.Result, error) {
	return load.Load(context.Background(), "testdata/simple", slog.New(slog.DiscardHandler))
})

var fixtureCallGraph = sync.OnceValues(func() (*CallGraph, error) {
	res, err := fixtureLoad()
	if err != nil {
		return nil, err
	}
	return BuildCallGraph(res), nil
})

func loadFixture(t *testing.T) *load.Result {
	t.Helper()
	res, err := fixtureLoad()
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func buildFixtureCallGraph(t *testing.T) *CallGraph {
	t.Helper()
	g, err := fixtureCallGraph()
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Functions) == 0 && len(g.Calls) == 0 {
		t.Fatal("no functions or calls found")
	}
	return g
}

func TestCallGraphIsSorted(t *testing.T) {
	g := buildFixtureCallGraph(t)

	if !slices.IsSortedFunc(g.Functions, func(a, b Function) int { return cmp.Compare(a.ID, b.ID) }) {
		t.Error("Functions are not sorted by ID")
	}
	if !slices.IsSortedFunc(g.Types, func(a, b Type) int { return cmp.Compare(a.ID, b.ID) }) {
		t.Error("Types are not sorted by ID")
	}
	if !slices.IsSortedFunc(g.Variables, func(a, b Variable) int { return cmp.Compare(a.ID, b.ID) }) {
		t.Error("Variables are not sorted by ID")
	}
	if !slices.IsSortedFunc(g.Implementations, func(a, b Implementation) int {
		return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.Interface, b.Interface))
	}) {
		t.Error("Implementations are not sorted by Type, then Interface")
	}
	if !slices.IsSortedFunc(g.Calls, func(a, b FunctionCall) int {
		return cmp.Or(cmp.Compare(a.Caller, b.Caller), cmp.Compare(a.Pos, b.Pos), cmp.Compare(a.Callee, b.Callee))
	}) {
		t.Error("Calls are not sorted by Caller, then Pos, then Callee")
	}
	if !slices.IsSortedFunc(g.Accesses, func(a, b Access) int {
		return cmp.Or(cmp.Compare(a.Accessor, b.Accessor), cmp.Compare(a.Pos, b.Pos), cmp.Compare(a.Target, b.Target), cmp.Compare(a.Kind, b.Kind))
	}) {
		t.Error("Accesses are not sorted by Accessor, then Pos, then Target, then Kind")
	}
}

// Each package's share of the graph depends only on that package and the
// repo's package paths. Built alone, it holds only nodes and edges in its own
// files, and the shares together are the whole graph. Implementations are
// matched across packages afterwards, so they're not in any share.
func TestBuildPackage(t *testing.T) {
	res := loadFixture(t)
	g := buildFixtureCallGraph(t)
	repo := map[string]bool{}
	for _, pkg := range res.Packages {
		repo[pkg.PkgPath] = true
	}

	var types []Type
	var variables []Variable
	var functions []Function
	var calls []FunctionCall
	var accesses []Access
	for _, pkg := range res.Packages {
		b := buildPackage(pkg, res.Dir, repo)

		dir, err := filepath.Rel(res.Dir, filepath.Dir(pkg.GoFiles[0]))
		if err != nil {
			t.Fatal(err)
		}
		inPkg := func(what, id, pos string) {
			file, _, _ := strings.Cut(pos, ":")
			if path.Dir(file) != filepath.ToSlash(dir) {
				t.Errorf("%s: %s %s at %s is outside the package", pkg.PkgPath, what, id, pos)
			}
		}
		for _, x := range b.types {
			inPkg("type", x.ID, x.Pos)
		}
		for _, x := range b.variables {
			inPkg("variable", x.ID, x.Pos)
		}
		for _, x := range b.functions {
			inPkg("function", x.ID, x.Pos)
		}
		for _, x := range b.calls {
			inPkg("call from", x.Caller, x.Pos)
		}
		for _, x := range b.accesses {
			inPkg("access by", x.Accessor, x.Pos)
		}

		types = append(types, b.types...)
		variables = append(variables, b.variables...)
		functions = append(functions, b.functions...)
		calls = append(calls, b.calls...)
		accesses = append(accesses, b.accesses...)
	}

	checkExact(t, "type", types, g.Types, func(ty Type) string { return fmt.Sprintf("%+v", ty) })
	checkExact(t, "variable", variables, g.Variables, func(v Variable) string { return fmt.Sprintf("%+v", v) })
	checkExact(t, "function", functions, g.Functions, formatFunction)
	checkExact(t, "call", calls, g.Calls, formatCall)
	checkExact(t, "access", accesses, g.Accesses, formatAccess)
}

// The graph doesn't depend on the order packages are built in: cached ones
// will come back in any order.
func TestBuildCallGraphPackageOrder(t *testing.T) {
	res := loadFixture(t)
	reversed := *res
	reversed.Packages = slices.Clone(res.Packages)
	slices.Reverse(reversed.Packages)

	want, err := json.Marshal(buildFixtureCallGraph(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(BuildCallGraph(&reversed))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("the graph changes when packages are built in reverse order")
	}
}

// Every local type in the graph got its ID from the package declaring it.
// typeID falls back to "pkg.Name#?" for one it wasn't given, which would mean
// a package's share needs another package's local types.
func TestCallGraphLocalTypesResolved(t *testing.T) {
	data, err := json.Marshal(buildFixtureCallGraph(t))
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(data, []byte("#?")); n > 0 {
		t.Errorf("%d unresolved local type IDs (#?) in the graph", n)
	}
}

// Positions in the fixture files most tests cover.
func rat(line string) string { return "store/reads.go:" + line }
func wat(line string) string { return "store/writes.go:" + line }
func cat(line string) string { return "store/closures.go:" + line }
func iat(line string) string { return "store/impls.go:" + line }

// checkExact fails for each want missing from got and each got not in want,
// counting duplicates. Keys start with the ID, so a node with a wrong field
// shows as a missing and an unexpected line next to each other. %v prints nil
// and empty slices the same, so either is fine in want.
func checkExact[T any](t *testing.T, what string, got, want []T, key func(T) string) {
	t.Helper()
	n := make(map[string]int)
	for _, x := range got {
		n[key(x)]++
	}
	for _, x := range want {
		n[key(x)]--
	}
	for _, k := range slices.Sorted(maps.Keys(n)) {
		for range -n[k] {
			t.Errorf("missing %s %s", what, k)
		}
		for range n[k] {
			t.Errorf("unexpected %s %s", what, k)
		}
	}
}

func formatAccess(a Access) string {
	s := fmt.Sprintf("%s %s %s at %s", a.Accessor, a.Kind, a.Target, a.Pos)
	if a.Func != "" {
		s += " func=" + a.Func
	}
	return s
}
