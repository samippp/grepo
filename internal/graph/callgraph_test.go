// Expected BuildCallGraph output for testdata/simple: the shared fixture IDs
// and helpers. Each node and edge kind has its own file, like the code.

package graph

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
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
)

// fixtureCallGraph loads and builds testdata/simple once for every test.
// Tests only read the graph.
var fixtureCallGraph = sync.OnceValues(func() (*CallGraph, error) {
	res, err := load.Load(context.Background(), "testdata/simple", slog.New(slog.DiscardHandler))
	if err != nil {
		return nil, err
	}
	return BuildCallGraph(res), nil
})

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

// Positions in the fixture files most tests cover.
func rat(line string) string { return "store/reads.go:" + line }
func wat(line string) string { return "store/writes.go:" + line }
func cat(line string) string { return "store/closures.go:" + line }

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
