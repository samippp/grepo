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
			Owner:    tStore,
		},
		{
			ID: idFirst, Name: "first", Package: mainPkg,
			Params:  []Param{{Name: "xs", Type: "[]T"}},
			Results: []Param{{Name: "", Type: "T"}},
			Pos:     "main.go:39",
		},
		{
			ID: idPair, Name: "pair", Package: mainPkg,
			Params: []Param{{Name: "k", Type: "K"}, {Name: "v", Type: "V"}},
			Pos:    "main.go:41",
		},
		{
			ID: idGenerics, Name: "generics", Package: mainPkg,
			Pos: "main.go:44",
		},
		{
			ID: idPush, Name: "Push", Package: mainPkg,
			Receiver: &Param{Name: "l", Type: "*" + mainPkg + ".List[T]"},
			Params:   []Param{{Name: "v", Type: "T"}},
			Exported: true,
			Pos:      "main.go:56",
			Owner:    tList,
		},
		{
			ID: idUseList, Name: "useList", Package: mainPkg,
			Pos: "main.go:58",
		},
		// Several inits per package: file + position makes each ID unique (#6).
		{ID: idInit1, Name: "init", Package: mainPkg, Pos: "main.go:64"},
		{ID: idInit2, Name: "init", Package: mainPkg, Pos: "main.go:66"},
		{ID: idStoreInit, Name: "init", Package: storePkg, Pos: "store/store.go:29"},
		// A method named init is an ordinary method: normal ID.
		{
			ID: idListInit, Name: "init", Package: mainPkg,
			Receiver: &Param{Name: "l", Type: "*" + mainPkg + ".List[T]"},
			Pos:      "main.go:68",
			Owner:    tList,
		},
		// Value receiver on a non-struct type.
		{
			ID: "(" + tUserID + ").String", Name: "String", Package: modelPkg,
			Receiver: &Param{Name: "u", Type: tUserID},
			Results:  []Param{{Name: "", Type: "string"}},
			Exported: true,
			Pos:      "model/kinds.go:10",
			Owner:    tUserID,
		},
		// Interface methods: explicit ones only, owned by their interface, no receiver.
		{
			ID: "(" + tGetter + ").Get", Name: "Get", Package: storePkg,
			Params:   []Param{{Name: "id", Type: "int"}},
			Results:  []Param{{Name: "", Type: tUser}},
			Exported: true,
			Pos:      "store/store.go:13",
			Owner:    tGetter,
		},
		{
			ID: "(" + tReadNamer + ").Name", Name: "Name", Package: modelPkg,
			Results:  []Param{{Name: "", Type: "string"}},
			Exported: true,
			Pos:      "model/kinds.go:15",
			Owner:    tReadNamer,
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

func TestCallGraphTypes(t *testing.T) {
	g := buildFixtureCallGraph(t)

	want := []Type{
		{
			ID: tStore, Name: "Store", Package: storePkg, Kind: StructType, Exported: true,
			Pos: "store/store.go:16",
			Fields: []Field{
				{ID: tStore + ".mu", Name: "mu", Type: "sync.Mutex", Pos: "store/store.go:17"},
				{ID: tStore + ".users", Name: "users", Type: "map[int]" + tUser, Pos: "store/store.go:18"},
			},
		},
		{
			ID: tGetter, Name: "Getter", Package: storePkg, Kind: InterfaceType, Exported: true,
			Pos: "store/store.go:12",
		},
		{
			ID: tList, Name: "List", Package: mainPkg, Kind: StructType, Exported: true,
			Pos:    "main.go:54",
			Fields: []Field{{ID: tList + ".items", Name: "items", Type: "[]T", Pos: "main.go:54"}},
		},
		{
			ID: tUser, Name: "User", Package: modelPkg, Kind: StructType, Exported: true,
			Pos: "model/model.go:4",
			Fields: []Field{
				{ID: tUser + ".ID", Name: "ID", Type: "int", Pos: "model/model.go:5"},
				{ID: tUser + ".Name", Name: "Name", Type: "string", Pos: "model/model.go:6"},
			},
		},
		// Embedded struct field: named after its type, Embedded set.
		{
			ID: tTagged, Name: "Tagged", Package: modelPkg, Kind: StructType, Exported: true,
			Pos: "model/kinds.go:18",
			Fields: []Field{
				{ID: tTagged + ".User", Name: "User", Type: tUser, Embedded: true, Pos: "model/kinds.go:19"},
				{ID: tTagged + ".Tag", Name: "Tag", Type: "string", Pos: "model/kinds.go:20"},
			},
		},
		// Alias: Target is what it names; no fields or methods of its own.
		{
			ID: tID, Name: "ID", Package: modelPkg, Kind: AliasType, Exported: true,
			Pos: "model/kinds.go:6", Target: "int",
		},
		// Neither struct nor interface: Underlying says what it is.
		{
			ID: tUserID, Name: "UserID", Package: modelPkg, Kind: OtherType, Exported: true,
			Pos: "model/kinds.go:8", Underlying: "int",
		},
		// Embedded interface: recorded in Embeds, its methods are not copied.
		{
			ID: tReadNamer, Name: "ReadNamer", Package: modelPkg, Kind: InterfaceType, Exported: true,
			Pos: "model/kinds.go:13", Embeds: []string{"io.Reader"},
		},
	}

	got := make(map[string]Type)
	for _, ty := range g.Types {
		got[ty.ID] = ty
	}
	for _, w := range want {
		ty, ok := got[w.ID]
		if !ok {
			t.Errorf("missing type %s", w.ID)
			continue
		}
		if !equalType(ty, w) {
			t.Errorf("type %s:\n got  %+v\n want %+v", w.ID, ty, w)
		}
		delete(got, w.ID)
	}
	for id := range got {
		t.Errorf("unexpected type %s", id)
	}
}

func TestCallGraphCalls(t *testing.T) {
	g := buildFixtureCallGraph(t)

	want := []FunctionCall{
		// store.New(): function in another package.
		{Caller: idMain, Callee: idNew, CallerPosition: "main.go:16", CallKind: StaticCall},
		// s.Get(1) where s is *Store: method on a concrete type.
		{Caller: idMain, Callee: idGet, CallerPosition: "main.go:17", CallKind: StaticCall,
			Args: []Arg{{"1", "int"}}},
		// g.Get(2) where g is store.Getter: points at the interface method.
		{Caller: idMain, Callee: "(" + storePkg + ".Getter).Get", CallerPosition: "main.go:20", CallKind: InterfaceCall,
			Args: []Arg{{"2", "int"}}},
		// f(u.Name) where f := greet: a function value, target unknown.
		{Caller: idMain, Callee: "", CallerPosition: "main.go:23", CallKind: DynamicCall,
			Args: []Arg{{"u.Name", "string"}}},
		// greet("closure") inside func(){...}(): belongs to main.
		{Caller: idMain, Callee: idGreet, CallerPosition: "main.go:26", CallKind: StaticCall,
			Args: []Arg{{`"closure"`, "string"}}},
		// Nothing for len(u.Name) (built-in) or int64(n) (conversion).
		// fmt.Println(u): outside the repo, but kept.
		{Caller: idMain, Callee: "fmt.Println", CallerPosition: "main.go:32", CallKind: StaticCall,
			Args: []Arg{{"u", "example.com/simple/model.User"}}},
		{Caller: idGreet, Callee: "fmt.Println", CallerPosition: "main.go:36", CallKind: StaticCall,
			Args: []Arg{{`"hello"`, "string"}, {"name", "string"}}},
		// s.mu.Lock(): method reached through a struct field.
		{Caller: idGet, Callee: "(*sync.Mutex).Lock", CallerPosition: "store/store.go:24", CallKind: StaticCall},
		// defer s.mu.Unlock(): deferred calls count too.
		{Caller: idGet, Callee: "(*sync.Mutex).Unlock", CallerPosition: "store/store.go:25", CallKind: StaticCall},
		// first[int](xs): explicit type arg (IndexExpr), target known (#4).
		{Caller: idGenerics, Callee: idFirst, CallerPosition: "main.go:46", CallKind: StaticCall,
			Args: []Arg{{"xs", "[]int"}}},
		// pair[string, int](...): two type args (IndexListExpr) (#4).
		{Caller: idGenerics, Callee: idPair, CallerPosition: "main.go:47", CallKind: StaticCall,
			Args: []Arg{{`"a"`, "string"}, {"1", "int"}}},
		// handlers[0](...): also an IndexExpr, but a func value, so dynamic.
		{Caller: idGenerics, Callee: "", CallerPosition: "main.go:50", CallKind: DynamicCall,
			Args: []Arg{{`"indexed"`, "string"}}},
		// l.Push(1) on List[int]: callee must be the declared List[T] method (#5).
		{Caller: idUseList, Callee: idPush, CallerPosition: "main.go:60", CallKind: StaticCall,
			Args: []Arg{{"1", "int"}}},
		// Calls from each init are attributed to that init (#6).
		{Caller: idInit1, Callee: idGreet, CallerPosition: "main.go:64", CallKind: StaticCall,
			Args: []Arg{{`"init 1"`, "string"}}},
		{Caller: idInit2, Callee: idGreet, CallerPosition: "main.go:66", CallKind: StaticCall,
			Args: []Arg{{`"init 2"`, "string"}}},
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
	if !slices.IsSortedFunc(g.Types, func(a, b Type) int { return cmp.Compare(a.ID, b.ID) }) {
		t.Error("Types are not sorted by ID")
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
		a.Exported == b.Exported && a.Pos == b.Pos && a.Owner == b.Owner
}

// equalType treats nil and empty slices as equal.
func equalType(a, b Type) bool {
	return a.ID == b.ID && a.Name == b.Name && a.Package == b.Package && a.Kind == b.Kind &&
		a.Exported == b.Exported && a.Pos == b.Pos &&
		slices.Equal(a.Fields, b.Fields) && slices.Equal(a.Embeds, b.Embeds) &&
		a.Target == b.Target && a.Underlying == b.Underlying
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
	return fmt.Sprintf("{name=%s pkg=%s recv=%s params=%v results=%v exported=%v pos=%s owner=%s}",
		f.Name, f.Package, recv, f.Params, f.Results, f.Exported, f.Pos, f.Owner)
}

func formatCall(c FunctionCall) string {
	return fmt.Sprintf("%s -> %q at %s (%s) args=%v", c.Caller, c.Callee, c.CallerPosition, c.CallKind, c.Args)
}
