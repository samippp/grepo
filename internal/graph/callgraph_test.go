// Expected BuildCallGraph output for testdata/simple.

package graph

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"testing"

	"github.com/samippp/grepo/internal/load"
)

const (
	mainPkg  = "example.com/simple"
	storePkg = "example.com/simple/store"

	idMain  = mainPkg + ".main"
	idGreet = mainPkg + ".greet"
	idNew   = storePkg + ".New"
	idGet   = "(*" + storePkg + ".Store).Get"
)

func buildFixtureCallGraph(t *testing.T) *CallGraph {
	t.Helper()
	res, err := load.Load(context.Background(), "testdata/simple", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	g := BuildCallGraph(res)
	if len(g.Functions) == 0 && len(g.Calls) == 0 {
		//t.Skip("BuildCallGraph not implemented yet")
		t.Error("Functions and calls not scanned. 0 for all")
	}
	return g
}

func TestCallGraphFunctions(t *testing.T) {
	g := buildFixtureCallGraph(t)

	want := []Function{
		{
			ID: idMain, Name: "main", Package: mainPkg,
			Pos: "main.go:15",
		},
		{
			ID: idGreet, Name: "greet", Package: mainPkg,
			Params: []Param{{Name: "name", Type: "string"}},
			Pos:    "main.go:35",
		},
		{
			ID: idNew, Name: "New", Package: storePkg,
			Results:  []Param{{Name: "", Type: "*" + storePkg + ".Store"}},
			Exported: true,
			Pos:      "store/store.go:21",
		},
		{
			ID: idGet, Name: "Get", Package: storePkg,
			Receiver: &Param{Name: "s", Type: "*" + storePkg + ".Store"},
			Params:   []Param{{Name: "id", Type: "int"}},
			Results:  []Param{{Name: "", Type: "example.com/simple/model.User"}},
			Exported: true,
			Pos:      "store/store.go:23",
		},
	}

	got := make(map[string]Function)
	for _, f := range g.Functions {
		got[f.ID] = f
	}
	for _, w := range want {
		f, ok := got[w.ID]
		if !ok {
			t.Errorf("missing function %s", w.ID)
			continue
		}
		if !equalFunction(f, w) {
			t.Errorf("function %s:\n got  %s\n want %s", w.ID, formatFunction(f), formatFunction(w))
		}
		delete(got, w.ID)
	}
	for id := range got {
		t.Errorf("unexpected function %s", id)
	}
}

func TestCallGraphCalls(t *testing.T) {
	g := buildFixtureCallGraph(t)

	want := []FunctionCall{
		// store.New(): function in another package.
		{Caller: idMain, Callee: idNew, CallerPosition: "main.go:16", CallKind: StaticCall},
		// s.Get(1) where s is *Store: method on a concrete type.
		{Caller: idMain, Callee: idGet, CallerPosition: "main.go:17", CallKind: StaticCall},
		// g.Get(2) where g is store.Getter: points at the interface method.
		{Caller: idMain, Callee: "(" + storePkg + ".Getter).Get", CallerPosition: "main.go:20", CallKind: InterfaceCall},
		// f(u.Name) where f := greet: a function value, target unknown.
		{Caller: idMain, Callee: "", CallerPosition: "main.go:23", CallKind: DynamicCall},
		// greet("closure") inside func(){...}(): belongs to main.
		{Caller: idMain, Callee: idGreet, CallerPosition: "main.go:26", CallKind: StaticCall},
		// Nothing for len(u.Name) (built-in) or int64(n) (conversion).
		// fmt.Println(u): outside the repo, but kept.
		{Caller: idMain, Callee: "fmt.Println", CallerPosition: "main.go:32", CallKind: StaticCall},
		{Caller: idGreet, Callee: "fmt.Println", CallerPosition: "main.go:36", CallKind: StaticCall},
		// s.mu.Lock(): method reached through a struct field.
		{Caller: idGet, Callee: "(*sync.Mutex).Lock", CallerPosition: "store/store.go:24", CallKind: StaticCall},
		// defer s.mu.Unlock(): deferred calls count too.
		{Caller: idGet, Callee: "(*sync.Mutex).Unlock", CallerPosition: "store/store.go:25", CallKind: StaticCall},
	}

	got := make(map[string]bool)
	for _, c := range g.Calls {
		got[formatCall(c)] = true
	}
	for _, w := range want {
		key := formatCall(w)
		if !got[key] {
			t.Errorf("missing call %s", key)
		}
		delete(got, key)
	}
	for key := range got {
		t.Errorf("unexpected call %s", key)
	}
}

func TestCallGraphIsSorted(t *testing.T) {
	g := buildFixtureCallGraph(t)

	if !slices.IsSortedFunc(g.Functions, func(a, b Function) int { return cmp.Compare(a.ID, b.ID) }) {
		t.Error("Functions are not sorted by ID")
	}
	if !slices.IsSortedFunc(g.Calls, func(a, b FunctionCall) int {
		return cmp.Or(cmp.Compare(a.Caller, b.Caller), cmp.Compare(a.CallerPosition, b.CallerPosition), cmp.Compare(a.Callee, b.Callee))
	}) {
		t.Error("Calls are not sorted by Caller, then CallerPosition, then Callee")
	}
}

// equalFunction treats nil and empty slices as equal.
func equalFunction(a, b Function) bool {
	return a.ID == b.ID && a.Name == b.Name && a.Package == b.Package &&
		equalParam(a.Receiver, b.Receiver) &&
		slices.Equal(a.Params, b.Params) && slices.Equal(a.Results, b.Results) &&
		a.Exported == b.Exported && a.Pos == b.Pos
}

func equalParam(a, b *Param) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func formatFunction(f Function) string {
	recv := "<nil>"
	if f.Receiver != nil {
		recv = fmt.Sprint(*f.Receiver)
	}
	return fmt.Sprintf("{name=%s pkg=%s recv=%s params=%v results=%v exported=%v pos=%s}",
		f.Name, f.Package, recv, f.Params, f.Results, f.Exported, f.Pos)
}

func formatCall(c FunctionCall) string {
	return fmt.Sprintf("%s -> %q at %s (%s)", c.Caller, c.Callee, c.CallerPosition, c.CallKind)
}
