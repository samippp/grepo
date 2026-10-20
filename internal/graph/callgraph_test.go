// Expected BuildCallGraph output for testdata/simple.

package graph

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
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
	tJob         = mainPkg + ".spawn.job#1"
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

	idValues = "(*" + tRHolder + ").values"
	idCalls  = "(*" + tRHolder + ").calls"
	idHands  = "(*" + tRHolder + ").hands"
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

	// Holder's unexported methods and the helpers in store/reads.go.
	holderMethod := func(name, line string, params, results []Param) Function {
		return Function{
			ID: "(*" + tRHolder + ")." + name, Name: name, Package: storePkg,
			Receiver: &Param{Name: "r", Type: "*" + tRHolder},
			Params:   params, Results: results,
			Pos: "store/reads.go:" + line, Owner: tRHolder,
		}
	}
	readsFunc := func(name, line string, params, results []Param) Function {
		return Function{ID: storePkg + "." + name, Name: name, Package: storePkg, Params: params, Results: results, Pos: "store/reads.go:" + line}
	}
	ints := []Param{{Name: "", Type: "int"}}

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
		{
			ID: storePkg + ".load", Name: "load", Package: storePkg,
			Results: []Param{{Name: "", Type: "int"}, {Name: "", Type: "error"}},
			Pos:     "store/vars.go:13",
		},
		{
			ID: storePkg + ".register", Name: "register", Package: storePkg,
			Results: []Param{{Name: "", Type: "bool"}},
			Pos:     "store/vars.go:19",
		},
		{
			ID: "(" + tCache + ").Get", Name: "Get", Package: storePkg,
			Receiver: &Param{Name: "c", Type: tCache},
			Params:   []Param{{Name: "id", Type: "int"}},
			Results:  []Param{{Name: "", Type: tUser}},
			Exported: true,
			Pos:      "store/cache.go:9",
			Owner:    tCache,
		},
		{
			ID: "(" + tNamer + ").Name", Name: "Name", Package: modelPkg,
			Results:  []Param{{Name: "", Type: "string"}},
			Exported: true,
			Pos:      "model/kinds.go:24",
			Owner:    tNamer,
		},
		// Unnamed receiver: func (Label) Name().
		{
			ID: "(" + tLabel + ").Name", Name: "Name", Package: modelPkg,
			Receiver: &Param{Name: "", Type: tLabel},
			Results:  []Param{{Name: "", Type: "string"}},
			Exported: true,
			Pos:      "model/kinds.go:28",
			Owner:    tLabel,
		},
		// Method IDs use the owner's type ID. A generic interface method has no
		// constraint in its ID (not Source[T any]).
		{
			ID: "(" + tSource + ").Next", Name: "Next", Package: mainPkg,
			Results:  []Param{{Name: "", Type: "T"}},
			Exported: true,
			Pos:      "main.go:71",
			Owner:    tSource,
		},
		{
			ID: mainPkg + ".drain", Name: "drain", Package: mainPkg,
			Params: []Param{{Name: "s", Type: mainPkg + ".Source[int]"}},
			Pos:    "main.go:73",
		},
		// Receiver written as List[E]: the ID still uses the declaration's List[T].
		{
			ID: "(*" + tList + ").Len", Name: "Len", Package: mainPkg,
			Receiver: &Param{Name: "l", Type: "*" + mainPkg + ".List[E]"},
			Results:  []Param{{Name: "", Type: "int"}},
			Exported: true,
			Pos:      "main.go:75",
			Owner:    tList,
		},
		// Declared through the alias S: the ID and owner are Store's.
		{
			ID: "(*" + tStore + ").Reset", Name: "Reset", Package: storePkg,
			Receiver: &Param{Name: "s", Type: "*" + tS},
			Exported: true,
			Pos:      "store/alias.go:6",
			Owner:    tStore,
		},
		{
			ID: mainPkg + ".describe", Name: "describe", Package: mainPkg,
			Params:  []Param{{Name: "err", Type: "error"}},
			Results: []Param{{Name: "", Type: "string"}},
			Pos:     "main.go:78",
		},
		{ID: mainPkg + ".parse", Name: "parse", Package: mainPkg, Pos: "main.go:83"},
		{
			ID: mainPkg + ".save", Name: "save", Package: mainPkg,
			Params: []Param{{Name: "ok", Type: "bool"}},
			Pos:    "main.go:87",
		},
		{
			ID: mainPkg + ".announce", Name: "announce", Package: mainPkg,
			Params: []Param{{Name: "v", Type: "any"}},
			Pos:    "main.go:98",
		},
		// A local interface's method: owned by the local type, ID built from its ID.
		{
			ID: "(" + tLocalNamer + ").Name", Name: "Name", Package: mainPkg,
			Results:  []Param{{Name: "", Type: "string"}},
			Exported: true,
			Pos:      "main.go:99",
			Owner:    tLocalNamer,
		},
		{ID: mainPkg + ".spawn", Name: "spawn", Package: mainPkg, Pos: "main.go:106"},
		{
			ID: mainPkg + ".wrap", Name: "wrap", Package: mainPkg,
			Params: []Param{{Name: "v", Type: "T"}},
			Pos:    "main.go:113",
		},
		{
			ID: "(*" + tWBox + ").Writes", Name: "Writes", Package: storePkg,
			Receiver: &Param{Name: "b", Type: "*" + tWBox},
			Params:   []Param{{Name: "o", Type: "*" + tWBox}, {Name: "src", Type: "[]byte"}},
			Exported: true,
			Pos:      "store/writes.go:30",
			Owner:    tWBox,
		},
		{ID: storePkg + ".locals", Name: "locals", Package: storePkg, Pos: "store/writes.go:52"},
		{
			ID: storePkg + ".build", Name: "build", Package: storePkg,
			Results: []Param{{Name: "", Type: "*" + tWBox}},
			Pos:     "store/writes.go:60",
		},
		{
			ID: storePkg + ".points", Name: "points", Package: storePkg,
			Results: []Param{{Name: "", Type: "[]" + tWInner}},
			Pos:     "store/writes.go:64",
		},
		{
			ID: storePkg + ".outside", Name: "outside", Package: storePkg,
			Results: []Param{{Name: "", Type: "*net/http.Server"}},
			Pos:     "store/writes.go:71",
		},
		{
			ID: "(*" + tWBox + ").ranges", Name: "ranges", Package: storePkg,
			Receiver: &Param{Name: "b", Type: "*" + tWBox},
			Params:   []Param{{Name: "xs", Type: "[]int"}},
			Pos:      "store/writes.go:77",
			Owner:    tWBox,
		},
		{
			ID: "(*" + tWBox + ").derefs", Name: "derefs", Package: storePkg,
			Receiver: &Param{Name: "b", Type: "*" + tWBox},
			Pos:      "store/writes.go:83",
			Owner:    tWBox,
		},
		{ID: storePkg + ".promoted", Name: "promoted", Package: storePkg, Pos: "store/writes.go:93"},

		{
			ID: "(*" + tRPart + ").Bump", Name: "Bump", Package: storePkg,
			Receiver: &Param{Name: "p", Type: "*" + tRPart},
			Exported: true, Pos: "store/reads.go:13", Owner: tRPart,
		},
		{
			ID: "(" + tRPart + ").Value", Name: "Value", Package: storePkg,
			Receiver: &Param{Name: "p", Type: tRPart}, Results: ints,
			Exported: true, Pos: "store/reads.go:15", Owner: tRPart,
		},
		{
			ID: "(*" + tRHolder + ").Touch", Name: "Touch", Package: storePkg,
			Receiver: &Param{Name: "r", Type: "*" + tRHolder},
			Exported: true, Pos: "store/reads.go:36", Owner: tRHolder,
		},
		readsFunc("use", "54", []Param{{Name: "n", Type: "int"}}, nil),
		readsFunc("two", "56", []Param{{Name: "a", Type: "int"}, {Name: "b", Type: "int"}}, nil),
		readsFunc("sum", "58", []Param{{Name: "xs", Type: "[]int"}}, ints),
		readsFunc("keep", "60", []Param{{Name: "p", Type: "*" + tRPart}}, nil),
		holderMethod("values", "62", []Param{{Name: "xs", Type: "[]int"}}, ints),
		holderMethod("writes", "80", []Param{{Name: "v", Type: "string"}}, nil),
		holderMethod("addresses", "90", nil, nil),
		holderMethod("calls", "98", []Param{{Name: "w", Type: "net/http.ResponseWriter"}, {Name: "req", Type: "*net/http.Request"}}, nil),
		readsFunc("lock", "114", []Param{{Name: "l", Type: "*" + tRLocked}}, nil),
		holderMethod("outsides", "118", []Param{{Name: "req", Type: "*net/http.Request"}}, ints),
		holderMethod("hands", "127",
			[]Param{{Name: "other", Type: "*" + tRHolder}, {Name: "ch", Type: "chan []int"}},
			[]Param{{Name: "", Type: "map[int]string"}}),
		readsFunc("bagLen", "139", []Param{{Name: "b", Type: "*" + storePkg + ".Bag[int]"}}, ints),
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
		{
			ID: tCache, Name: "Cache", Package: storePkg, Kind: StructType, Exported: true,
			Pos: "store/cache.go:7",
		},
		{
			ID: tNamer, Name: "Namer", Package: modelPkg, Kind: InterfaceType, Exported: true,
			Pos: "model/kinds.go:24",
		},
		{
			ID: tLabel, Name: "Label", Package: modelPkg, Kind: StructType, Exported: true,
			Pos: "model/kinds.go:26",
		},
		{
			ID: tBadge, Name: "Badge", Package: modelPkg, Kind: StructType, Exported: true,
			Pos:    "model/kinds.go:30",
			Fields: []Field{{ID: tBadge + ".Label", Name: "Label", Type: tLabel, Embedded: true, Pos: "model/kinds.go:30"}},
		},
		{
			ID: tAny, Name: "Any", Package: modelPkg, Kind: InterfaceType, Exported: true,
			Pos: "model/kinds.go:33",
		},
		{
			ID: tSource, Name: "Source", Package: mainPkg, Kind: InterfaceType, Exported: true,
			Pos: "main.go:71",
		},
		{
			ID: tS, Name: "S", Package: storePkg, Kind: AliasType, Exported: true,
			Pos: "store/alias.go:4", Target: tStore,
		},
		// Package-level result keeps its plain ID; the local ones can't collide with it.
		{
			ID: tResult, Name: "result", Package: mainPkg, Kind: StructType,
			Pos:    "main.go:81",
			Fields: []Field{{ID: tResult + ".ok", Name: "ok", Type: "bool", Pos: "main.go:81"}},
		},
		{
			ID: tParseResult, Name: "result", Package: mainPkg, Kind: StructType,
			Pos:    "main.go:84",
			Fields: []Field{{ID: tParseResult + ".n", Name: "n", Type: "int", Pos: "main.go:84"}},
		},
		// Same name twice in one function: numbered in source order.
		{
			ID: tSaveResult1, Name: "result", Package: mainPkg, Kind: StructType,
			Pos:    "main.go:89",
			Fields: []Field{{ID: tSaveResult1 + ".err", Name: "err", Type: "error", Pos: "main.go:89"}},
		},
		{
			ID: tSaveResult2, Name: "result", Package: mainPkg, Kind: StructType,
			Pos:    "main.go:91",
			Fields: []Field{{ID: tSaveResult2 + ".msg", Name: "msg", Type: "string", Pos: "main.go:91"}},
		},
		{
			ID: tLocalNamer, Name: "namer", Package: mainPkg, Kind: InterfaceType,
			Pos: "main.go:99",
		},
		// Inside a closure: the parent is still spawn (#13).
		{
			ID: tJob, Name: "job", Package: mainPkg, Kind: StructType,
			Pos:    "main.go:108",
			Fields: []Field{{ID: tJob + ".Store", Name: "Store", Type: "*" + tStore, Embedded: true, Pos: "main.go:108"}},
		},
		// Uses wrap's T, but has no type params of its own: no [T] in the ID.
		{
			ID: tBox, Name: "box", Package: mainPkg, Kind: StructType,
			Pos:    "main.go:114",
			Fields: []Field{{ID: tBox + ".v", Name: "v", Type: "T", Pos: "main.go:114"}},
		},
		{
			ID: tWInner, Name: "Inner", Package: storePkg, Kind: StructType, Exported: true,
			Pos:    "store/writes.go:12",
			Fields: []Field{{ID: tWInner + ".X", Name: "X", Type: "int", Pos: "store/writes.go:12"}},
		},
		{
			ID: tWBox, Name: "Box", Package: storePkg, Kind: StructType, Exported: true,
			Pos: "store/writes.go:14",
			Fields: []Field{
				{ID: tWBox + ".N", Name: "N", Type: "int", Pos: "store/writes.go:15"},
				{ID: tWBox + ".Name", Name: "Name", Type: "string", Pos: "store/writes.go:16"},
				{ID: tWBox + ".Items", Name: "Items", Type: "[]int", Pos: "store/writes.go:17"},
				{ID: tWBox + ".Inner", Name: "Inner", Type: tWInner, Pos: "store/writes.go:18"},
				{ID: tWBox + ".Ptr", Name: "Ptr", Type: "*" + tWInner, Pos: "store/writes.go:19"},
				{ID: tWBox + ".Buf", Name: "Buf", Type: "[4]byte", Pos: "store/writes.go:20"},
				{ID: tWBox + ".Users", Name: "Users", Type: "map[int]" + tUser, Pos: "store/writes.go:21"},
				{ID: tWBox + ".Tags", Name: "Tags", Type: "[]" + tTagged, Pos: "store/writes.go:22"},
			},
		},
		{
			ID: storePkg + ".Wrapped", Name: "Wrapped", Package: storePkg, Kind: StructType, Exported: true,
			Pos:    "store/writes.go:89",
			Fields: []Field{{ID: storePkg + ".Wrapped.Inner", Name: "Inner", Type: "*" + tWInner, Embedded: true, Pos: "store/writes.go:89"}},
		},

		{
			ID: tRPart, Name: "Part", Package: storePkg, Kind: StructType, Exported: true,
			Pos:    "store/reads.go:11",
			Fields: []Field{{ID: tRPart + ".X", Name: "X", Type: "int", Pos: "store/reads.go:11"}},
		},
		{
			ID: tRBase, Name: "Base", Package: storePkg, Kind: StructType, Exported: true,
			Pos:    "store/reads.go:17",
			Fields: []Field{{ID: tRBase + ".ID", Name: "ID", Type: "int", Pos: "store/reads.go:17"}},
		},
		{
			ID: tRSub, Name: "Sub", Package: storePkg, Kind: StructType, Exported: true,
			Pos:    "store/reads.go:19",
			Fields: []Field{{ID: tRSub + ".Ptr", Name: "Ptr", Type: "*" + tRPart, Pos: "store/reads.go:19"}},
		},
		{
			ID: tRHolder, Name: "Holder", Package: storePkg, Kind: StructType, Exported: true,
			Pos: "store/reads.go:21",
			Fields: []Field{
				{ID: tRHolder + ".Base", Name: "Base", Type: tRBase, Embedded: true, Pos: "store/reads.go:22"},
				{ID: tRHolder + ".N", Name: "N", Type: "int", Pos: "store/reads.go:23"},
				{ID: tRHolder + ".Step", Name: "Step", Type: "int", Pos: "store/reads.go:24"},
				{ID: tRHolder + ".Key", Name: "Key", Type: "int", Pos: "store/reads.go:25"},
				{ID: tRHolder + ".Items", Name: "Items", Type: "[]int", Pos: "store/reads.go:26"},
				{ID: tRHolder + ".Users", Name: "Users", Type: "map[int]string", Pos: "store/reads.go:27"},
				{ID: tRHolder + ".Val", Name: "Val", Type: tRPart, Pos: "store/reads.go:28"},
				{ID: tRHolder + ".Ptr", Name: "Ptr", Type: "*" + tRPart, Pos: "store/reads.go:29"},
				{ID: tRHolder + ".Sub", Name: "Sub", Type: tRSub, Pos: "store/reads.go:30"},
				{ID: tRHolder + ".H", Name: "H", Type: "net/http.Handler", Pos: "store/reads.go:31"},
				{ID: tRHolder + ".OnErr", Name: "OnErr", Type: "func(error)", Pos: "store/reads.go:32"},
				{ID: tRHolder + ".mu", Name: "mu", Type: "sync.Mutex", Pos: "store/reads.go:33"},
			},
		},
		{
			ID: tRLocked, Name: "Locked", Package: storePkg, Kind: StructType, Exported: true,
			Pos:    "store/reads.go:39",
			Fields: []Field{{ID: tRLocked + ".Mutex", Name: "Mutex", Type: "sync.Mutex", Embedded: true, Pos: "store/reads.go:39"}},
		},
		{
			ID: tRBag, Name: "Bag", Package: storePkg, Kind: StructType, Exported: true,
			Pos:    "store/reads.go:41",
			Fields: []Field{{ID: tRBag + ".items", Name: "items", Type: "[]T", Pos: "store/reads.go:41"}},
		},
		{
			ID: storePkg + ".Slots", Name: "Slots", Package: storePkg, Kind: OtherType, Exported: true,
			Pos: "store/reads.go:44", Underlying: "[100]byte",
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

func TestCallGraphImplementations(t *testing.T) {
	g := buildFixtureCallGraph(t)

	want := []Implementation{
		// Value receiver: Cache and *Cache both implement Getter.
		{Type: tCache, Interface: tGetter},
		// Pointer receiver: only *Store implements Getter.
		{Type: tStore, Interface: tGetter, PointerOnly: true},
		{Type: tLabel, Interface: tNamer},
		// Badge has Name only through its embedded Label (a promoted method).
		{Type: tBadge, Interface: tNamer},
		// Local interfaces take part like package-level ones.
		{Type: tLabel, Interface: tLocalNamer},
		{Type: tBadge, Interface: tLocalNamer},
		// A local struct implements through its embedded *Store, so its value does too.
		{Type: tJob, Interface: tGetter},
		// Nothing for Any: empty interfaces are skipped.
		// Not listed: interfaces themselves, generic List[T] (no type args to check),
		// and interfaces outside the repo (UserID implements fmt.Stringer).
	}

	got := make(map[Implementation]bool)
	for _, im := range g.Implementations {
		got[im] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing implementation %+v", w)
		}
		delete(got, w)
	}
	for im := range got {
		t.Errorf("unexpected implementation %+v", im)
	}
}

func TestCallGraphVariables(t *testing.T) {
	g := buildFixtureCallGraph(t)

	v := func(name, typ, pos string) Variable {
		return Variable{ID: storePkg + "." + name, Name: name, Package: storePkg, Type: typ, Pos: "store/vars.go:" + pos}
	}
	c := func(name, typ, value, pos string, exported bool) Variable {
		x := v(name, typ, pos)
		x.Const, x.Value, x.Exported = true, value, exported
		return x
	}
	timeout := v("Timeout", "int", "6")
	timeout.Exported = true

	want := []Variable{
		v("defaultStore", "*"+tStore, "4"),
		timeout,
		// var a, b = 1, 2: one node per name.
		v("a", "int", "8"),
		v("b", "int", "8"),
		// var first, err = load(): one node per name, one call.
		v("first", "int", "11"),
		v("err", "error", "11"),
		// No nodes for the two "_" vars on lines 16-17.
		c("maxUsers", "untyped int", "100", "21", false),
		c("greeting", "string", `"hi"`, "23", false),
		// iota: Green and Blue have no value in the source, but the type checker knows it.
		c("Red", "untyped int", "0", "26", true),
		c("Green", "untyped int", "1", "27", true),
		c("Blue", "untyped int", "2", "28", true),
		// No node for save's local var count.
		{ID: storePkg + ".Counter", Name: "Counter", Package: storePkg, Type: "int", Exported: true, Pos: "store/writes.go:25"},
		{ID: storePkg + ".defaultBox", Name: "defaultBox", Package: storePkg, Type: tWBox, Pos: "store/writes.go:28"},
		{ID: storePkg + ".wrapped", Name: "wrapped", Package: storePkg, Type: storePkg + ".Wrapped", Pos: "store/writes.go:91"},
		{ID: storePkg + ".limit", Name: "limit", Package: storePkg, Type: "int", Pos: "store/reads.go:46"},
		{ID: storePkg + ".doubled", Name: "doubled", Package: storePkg, Type: "untyped int", Const: true, Value: "200", Pos: "store/reads.go:48"},
		{ID: storePkg + ".shared", Name: "shared", Package: storePkg, Type: tRHolder, Pos: "store/reads.go:50"},
		{ID: storePkg + ".OnReady", Name: "OnReady", Package: storePkg, Type: "func()", Exported: true, Pos: "store/reads.go:52"},
	}

	got := make(map[string]Variable)
	for _, x := range g.Variables {
		got[x.ID] = x
	}
	for _, w := range want {
		x, ok := got[w.ID]
		if !ok {
			t.Errorf("missing variable %s", w.ID)
			continue
		}
		if x != w {
			t.Errorf("variable %s:\n got  %+v\n want %+v", w.ID, x, w)
		}
		delete(got, w.ID)
	}
	for id := range got {
		t.Errorf("unexpected variable %s", id)
	}
}

func TestCallGraphCalls(t *testing.T) {
	g := buildFixtureCallGraph(t)

	rat := func(line string) string { return "store/reads.go:" + line }

	want := []FunctionCall{
		// store.New(): function in another package.
		{Caller: idMain, Callee: idNew, Pos: "main.go:16", CallKind: StaticCall},
		// s.Get(1) where s is *Store: method on a concrete type.
		{Caller: idMain, Callee: idGet, Pos: "main.go:17", CallKind: StaticCall,
			Args: []Arg{{"1", "int"}}},
		// g.Get(2) where g is store.Getter: points at the interface method.
		{Caller: idMain, Callee: "(" + storePkg + ".Getter).Get", Pos: "main.go:20", CallKind: InterfaceCall,
			Args: []Arg{{"2", "int"}}},
		// f(u.Name) where f := greet: a function value, target unknown.
		{Caller: idMain, Callee: "", Pos: "main.go:23", CallKind: DynamicCall,
			Args: []Arg{{"u.Name", "string"}}},
		// greet("closure") inside func(){...}(): belongs to main.
		{Caller: idMain, Callee: idGreet, Pos: "main.go:26", CallKind: StaticCall,
			Args: []Arg{{`"closure"`, "string"}}},
		// Nothing for len(u.Name) (built-in) or int64(n) (conversion).
		// fmt.Println(u): outside the repo, but kept.
		{Caller: idMain, Callee: "fmt.Println", Pos: "main.go:32", CallKind: StaticCall,
			Args: []Arg{{"u", "example.com/simple/model.User"}}},
		{Caller: idGreet, Callee: "fmt.Println", Pos: "main.go:36", CallKind: StaticCall,
			Args: []Arg{{`"hello"`, "string"}, {"name", "string"}}},
		// s.mu.Lock(): method reached through a struct field.
		{Caller: idGet, Callee: "(*sync.Mutex).Lock", Pos: "store/store.go:24", CallKind: StaticCall},
		// defer s.mu.Unlock(): deferred calls count too.
		{Caller: idGet, Callee: "(*sync.Mutex).Unlock", Pos: "store/store.go:25", CallKind: StaticCall},
		// first[int](xs): explicit type arg (IndexExpr), target known (#4).
		{Caller: idGenerics, Callee: idFirst, Pos: "main.go:46", CallKind: StaticCall,
			Args: []Arg{{"xs", "[]int"}}},
		// pair[string, int](...): two type args (IndexListExpr) (#4).
		{Caller: idGenerics, Callee: idPair, Pos: "main.go:47", CallKind: StaticCall,
			Args: []Arg{{`"a"`, "string"}, {"1", "int"}}},
		// handlers[0](...): also an IndexExpr, but a func value, so dynamic.
		{Caller: idGenerics, Callee: "", Pos: "main.go:50", CallKind: DynamicCall,
			Args: []Arg{{`"indexed"`, "string"}}},
		// l.Push(1) on List[int]: callee must be the declared List[T] method (#5).
		{Caller: idUseList, Callee: idPush, Pos: "main.go:60", CallKind: StaticCall,
			Args: []Arg{{"1", "int"}}},
		// Calls from each init are attributed to that init (#6).
		{Caller: idInit1, Callee: idGreet, Pos: "main.go:64", CallKind: StaticCall,
			Args: []Arg{{`"init 1"`, "string"}}},
		{Caller: idInit2, Callee: idGreet, Pos: "main.go:66", CallKind: StaticCall,
			Args: []Arg{{`"init 2"`, "string"}}},
		// Package-level calls: the caller is the package itself (#7).
		{Caller: storePkg, Callee: idNew, Pos: "store/vars.go:4", CallKind: StaticCall},
		{Caller: storePkg, Callee: storePkg + ".load", Pos: "store/vars.go:11", CallKind: StaticCall},
		// var _ = register() has no variable node, but the call is still recorded.
		{Caller: storePkg, Callee: storePkg + ".register", Pos: "store/vars.go:17", CallKind: StaticCall},
		// Nothing for var _ Getter = (*Store)(nil): a conversion, not a call.
		// s.Next() on a generic interface: the callee matches the method node's ID.
		{Caller: mainPkg + ".drain", Callee: "(" + tSource + ").Next", Pos: "main.go:73", CallKind: InterfaceCall},
		// err.Error(): error is built in, with no package path in its ID.
		{Caller: mainPkg + ".describe", Callee: "(error).Error", Pos: "main.go:78", CallKind: InterfaceCall},
		// n.Name() through a local interface: the callee has a node now.
		{Caller: mainPkg + ".announce", Callee: "(" + tLocalNamer + ").Name", Pos: "main.go:101", CallKind: InterfaceCall},

		// The reads fixture's calls (store/reads.go). Their Through is in TestCallGraphThrough.
		{Caller: idValues, Callee: storePkg + ".use", Pos: rat("64"), CallKind: StaticCall, Args: []Arg{{"r.N", "int"}}},
		{Caller: idValues, Callee: storePkg + ".two", Pos: rat("75"), CallKind: StaticCall, Args: []Arg{{"r.N", "int"}, {"r.N", "int"}}},
		// No call for the func literal called on line 76.
		{Caller: idCalls, Callee: "(*" + tRPart + ").Bump", Pos: rat("99"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "(*" + tRPart + ").Bump", Pos: rat("100"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "(" + tRPart + ").Value", Pos: rat("101"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "(net/http.Handler).ServeHTTP", Pos: rat("102"), CallKind: InterfaceCall,
			Args: []Arg{{"w", "net/http.ResponseWriter"}, {"req", "*net/http.Request"}}},
		{Caller: idCalls, Callee: "", Pos: rat("103"), CallKind: DynamicCall, Args: []Arg{{"nil", "untyped nil"}}},
		{Caller: idCalls, Callee: "(*sync.Mutex).Lock", Pos: rat("104"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "(*" + tRHolder + ").Touch", Pos: rat("105"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "(*" + tRHolder + ").Touch", Pos: rat("106"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "", Pos: rat("107"), CallKind: DynamicCall},
		{Caller: idCalls, Callee: "", Pos: rat("110"), CallKind: DynamicCall},
		{Caller: idCalls, Callee: "", Pos: rat("111"), CallKind: DynamicCall},
		{Caller: storePkg + ".lock", Callee: "(*sync.Mutex).Lock", Pos: rat("115"), CallKind: StaticCall},
		{Caller: idHands, Callee: storePkg + ".sum", Pos: rat("128"), CallKind: StaticCall, Args: []Arg{{"r.Items", "[]int"}}},
		{Caller: idHands, Callee: storePkg + ".use", Pos: rat("128"), CallKind: StaticCall, Args: []Arg{{"sum(r.Items)", "int"}}},
		{Caller: idHands, Callee: storePkg + ".keep", Pos: rat("133"), CallKind: StaticCall, Args: []Arg{{"r.Ptr", "*" + tRPart}}},
		{Caller: idHands, Callee: storePkg + ".sum", Pos: rat("134"), CallKind: StaticCall, Args: []Arg{{"r.Items[1:]", "[]int"}}},
		{Caller: idHands, Callee: storePkg + ".use", Pos: rat("134"), CallKind: StaticCall, Args: []Arg{{"sum(r.Items[1:])", "int"}}},
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

func TestCallGraphAccesses(t *testing.T) {
	g := buildFixtureCallGraph(t)

	idWrites := "(*" + tWBox + ").Writes"
	w := func(accessor, target, pos string, kind AccessKind) Access {
		return Access{Accessor: accessor, Target: target, Pos: pos, Kind: kind}
	}
	at := func(line string) string { return "store/writes.go:" + line }
	rat := func(line string) string { return "store/reads.go:" + line }
	idWritesR := "(*" + tRHolder + ").writes"

	want := []Access{
		// Existing fixture code. Literals construct their fields; package vars with a value
		// are constructed by the package. Timeout has no value, so no edge.
		w(idNew, tStore+".users", "store/store.go:21", ConstructAccess),
		w("("+tCache+").Get", tUser+".ID", "store/cache.go:9", ConstructAccess),
		w(storePkg, storePkg+".defaultStore", "store/vars.go:4", ConstructAccess),
		w(storePkg, storePkg+".a", "store/vars.go:8", ConstructAccess),
		w(storePkg, storePkg+".b", "store/vars.go:8", ConstructAccess),
		w(storePkg, storePkg+".first", "store/vars.go:11", ConstructAccess),
		w(storePkg, storePkg+".err", "store/vars.go:11", ConstructAccess),
		// l.items = append(l.items, v) through List[T]: the generic field, and an update.
		w(idPush, tList+".items", "main.go:56", UpdateAccess),
		// A local type's field.
		w(mainPkg+".wrap", tBox+".v", "main.go:115", ConstructAccess),

		// var defaultBox = Box{Name: "d"}: the var and the literal's field.
		w(storePkg, storePkg+".defaultBox", at("28"), ConstructAccess),
		w(storePkg, tWBox+".Name", at("28"), ConstructAccess),

		w(idWrites, tWBox+".N", at("31"), AssignAccess),
		w(idWrites, tWBox+".N", at("32"), UpdateAccess), // ++
		w(idWrites, tWBox+".N", at("33"), UpdateAccess), // +=
		w(idWrites, tWBox+".N", at("34"), UpdateAccess), // b.N = b.N + 1: same expression on the right
		w(idWrites, tWBox+".N", at("35"), AssignAccess), // b.N = o.N + 1: same field ID, different object
		w(idWrites, tWBox+".Items", at("36"), UpdateAccess),
		// b.Users[1] = model.User{}: an empty literal constructs nothing.
		w(idWrites, tWBox+".Users", at("37"), ContentAccess),
		w(idWrites, tWBox+".Users", at("38"), ContentAccess),
		w(idWrites, tUser+".Name", at("38"), ConstructAccess),
		w(idWrites, tWBox+".Items", at("39"), ContentAccess), // b.Items[0]++: the index decides
		w(idWrites, tWBox+".Users", at("40"), ContentAccess), // delete
		w(idWrites, tWBox+".Items", at("41"), ContentAccess), // clear
		w(idWrites, tWBox+".Buf", at("42"), ContentAccess),   // copy(b.Buf[:], ...): a slice of the field
		w(idWrites, tWBox+".Buf", at("43"), ContentAccess),
		// b.Inner.X = 1: Inner is a struct value, so Box's memory changes too.
		w(idWrites, tWInner+".X", at("44"), AssignAccess),
		w(idWrites, tWBox+".Inner", at("44"), ContentAccess),
		// b.Ptr.X = 1: Ptr still holds the same address, but what it points to changed,
		// as with a slice or map. The walk stops there.
		w(idWrites, tWInner+".X", at("45"), AssignAccess),
		w(idWrites, tWBox+".Ptr", at("45"), ContentAccess),
		w(idWrites, tWBox+".Ptr", at("46"), ContentAccess), // *b.Ptr = Inner{}
		// b.Tags[0].Name = "t": Name is promoted from the embedded User value; the index
		// into Tags writes its contents and stops the path.
		w(idWrites, tUser+".Name", at("47"), AssignAccess),
		w(idWrites, tTagged+".User", at("47"), ContentAccess),
		w(idWrites, tWBox+".Tags", at("47"), ContentAccess),
		w(idWrites, storePkg+".Counter", at("48"), AssignAccess),
		// b.N, Counter = 4, 5: one edge per left-hand item.
		w(idWrites, tWBox+".N", at("49"), AssignAccess),
		w(idWrites, storePkg+".Counter", at("49"), AssignAccess),

		// Field writes count whatever the root; writes to locals themselves don't.
		// Nothing for local := Box{} (empty), p := &local, or *p = Box{}.
		w(storePkg+".locals", tWBox+".N", at("54"), AssignAccess),
		w(storePkg+".locals", tWBox+".N", at("55"), ConstructAccess),

		w(storePkg+".build", tWBox+".N", at("61"), ConstructAccess),
		w(storePkg+".build", tWBox+".Inner", at("61"), ConstructAccess),
		w(storePkg+".build", tWInner+".X", at("61"), ConstructAccess),
		// Elided types; {2} is positional.
		w(storePkg+".points", tWInner+".X", at("66"), ConstructAccess),
		w(storePkg+".points", tWInner+".X", at("67"), ConstructAccess),

		// Outside targets are kept. DefaultClient is a pointer: content on the var.
		w(storePkg+".outside", "os.Args", at("72"), AssignAccess),
		w(storePkg+".outside", "net/http.Client.Timeout", at("73"), AssignAccess),
		w(storePkg+".outside", "net/http.DefaultClient", at("73"), ContentAccess),
		w(storePkg+".outside", "net/http.Server.Addr", at("74"), ConstructAccess),

		// for b.N = range xs: assigned each iteration.
		w("(*"+tWBox+").ranges", tWBox+".N", at("78"), AssignAccess),

		// (*b.Ptr).X = 1: the same edges as b.Ptr.X = 1.
		w("(*"+tWBox+").derefs", tWInner+".X", at("84"), AssignAccess),
		w("(*"+tWBox+").derefs", tWBox+".Ptr", at("84"), ContentAccess),
		// wrapped.X = 1 through the embedded *Inner: content on the field holding the
		// pointer, then stop, so nothing on wrapped.
		w(storePkg+".promoted", tWInner+".X", at("94"), AssignAccess),
		w(storePkg+".promoted", storePkg+".Wrapped.Inner", at("94"), ContentAccess),

		// Writes in the reads fixture (store/reads.go).
		w("(*"+tRPart+").Bump", tRPart+".X", rat("13"), UpdateAccess),
		w(storePkg, storePkg+".limit", rat("46"), ConstructAccess),
		w(storePkg, storePkg+".OnReady", rat("52"), ConstructAccess),
		w(idWritesR, tRPart+".X", rat("81"), AssignAccess),
		w(idWritesR, tRHolder+".Val", rat("81"), ContentAccess),
		w(idWritesR, tRPart+".X", rat("82"), AssignAccess),
		w(idWritesR, tRHolder+".Ptr", rat("82"), ContentAccess),
		w(idWritesR, tRPart+".X", rat("83"), AssignAccess),
		w(idWritesR, tRSub+".Ptr", rat("83"), ContentAccess),
		w(idWritesR, tRHolder+".Users", rat("84"), ContentAccess),
		w(idWritesR, tRHolder+".N", rat("85"), UpdateAccess),
		w(idWritesR, tRHolder+".N", rat("86"), UpdateAccess),
		w(idWritesR, tRHolder+".Items", rat("87"), UpdateAccess),
		w(idHands, tRHolder+".Items", rat("130"), AssignAccess),
		w(idHands, tRHolder+".Items", rat("131"), ConstructAccess),
	}

	// Counted, so a duplicated edge shows up as unexpected. Reads and escapes
	// have their own test.
	got := make(map[string]int)
	for _, a := range g.Accesses {
		if a.Kind != ReadAccess && a.Kind != EscapeAccess {
			got[formatAccess(a)]++
		}
	}
	for _, w := range want {
		key := formatAccess(w)
		if got[key] == 0 {
			t.Errorf("missing access %s", key)
			continue
		}
		got[key]--
	}
	for key, n := range got {
		for range n {
			t.Errorf("unexpected access %s", key)
		}
	}
}

func TestCallGraphThrough(t *testing.T) {
	g := buildFixtureCallGraph(t)

	rat := func(line string) string { return "store/reads.go:" + line }
	type site struct{ caller, callee, pos string }

	// Through: the field or package var a call is made on, or takes its func from.
	// Calls on locals, params and results have none.
	want := map[site]string{
		{idGet, "(*sync.Mutex).Lock", "store/store.go:24"}:   tStore + ".mu",
		{idGet, "(*sync.Mutex).Unlock", "store/store.go:25"}: tStore + ".mu",

		{idCalls, "(*" + tRPart + ").Bump", rat("99")}:         tRHolder + ".Ptr",   // pointer field
		{idCalls, "(*" + tRPart + ").Bump", rat("100")}:        tRHolder + ".Val",   // value field, pointer method
		{idCalls, "(" + tRPart + ").Value", rat("101")}:        tRHolder + ".Val",   // value method
		{idCalls, "(net/http.Handler).ServeHTTP", rat("102")}:  tRHolder + ".H",     // interface field
		{idCalls, "", rat("103")}:                              tRHolder + ".OnErr", // the func stored in OnErr
		{idCalls, "(*sync.Mutex).Lock", rat("104")}:            tRHolder + ".mu",
		{idCalls, "(*" + tRHolder + ").Touch", rat("106")}:     storePkg + ".shared",  // a package var
		{idCalls, "", rat("107")}:                              storePkg + ".OnReady", // a package var holding a func
		{storePkg + ".lock", "(*sync.Mutex).Lock", rat("115")}: tRLocked + ".Mutex",   // promoted: the embedded field
		// None for r.Touch() (a param) or f() and g() (method values in locals).
	}

	got := make(map[site]string)
	for _, c := range g.Calls {
		if c.Through != "" {
			got[site{c.Caller, c.Callee, c.Pos}] = c.Through
		}
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("call %s -> %q at %s: through = %q, want %q", k.caller, k.callee, k.pos, got[k], w)
		}
		delete(got, k)
	}
	for k, v := range got {
		t.Errorf("unexpected through %q on call %s -> %q at %s", v, k.caller, k.callee, k.pos)
	}
}

// Reads and escapes, checked exactly for store/reads.go only: every other
// fixture file has reads too, and they aren't listed.
func TestCallGraphReads(t *testing.T) {
	g := buildFixtureCallGraph(t)

	rat := func(line string) string { return "store/reads.go:" + line }
	r := func(accessor, target, line string) Access {
		return Access{Accessor: accessor, Target: target, Pos: rat(line), Kind: ReadAccess}
	}
	e := func(accessor, target, line string) Access {
		return Access{Accessor: accessor, Target: target, Pos: rat(line), Kind: EscapeAccess}
	}
	idWritesR := "(*" + tRHolder + ").writes"
	idAddrs := "(*" + tRHolder + ").addresses"
	idOutsides := "(*" + tRHolder + ").outsides"
	maxUsers := storePkg + ".maxUsers"

	want := []Access{
		// Package-level: the package reads the const, in a type, a var and a const.
		r(storePkg, maxUsers, "44"),
		r(storePkg, maxUsers, "46"),
		r(storePkg, maxUsers, "48"),
		// p.X++ is an update only; p.X in Value is a read.
		r("("+tRPart+").Value", tRPart+".X", "15"),

		// values: plain uses, range, index, every hop, embedded hops, builtins.
		r(idValues, tRHolder+".N", "63"),
		r(idValues, tRHolder+".N", "64"),
		r(idValues, tRHolder+".N", "65"),
		r(idValues, tRHolder+".Items", "67"),
		r(idValues, tRHolder+".Items", "69"),
		r(idValues, tRHolder+".Key", "69"),
		r(idValues, tRPart+".X", "70"),
		r(idValues, tRHolder+".Val", "70"),
		r(idValues, tRPart+".X", "71"),
		r(idValues, tRHolder+".Ptr", "71"), // pointer hops are reads too
		r(idValues, tRBase+".ID", "72"),
		r(idValues, tRHolder+".Base", "72"), // r.ID is r.Base.ID
		r(idValues, tRHolder+".Items", "73"),
		r(idValues, tRHolder+".Items", "74"), // copy's source
		r(idValues, tRHolder+".N", "75"),
		r(idValues, tRHolder+".N", "75"),    // one edge per site: two on this line
		r(idValues, tRHolder+".Step", "76"), // in a closure: belongs to values

		// writes: no read where the path gets a write edge.
		// 81, 82: nothing. 83: the write stops at Sub.Ptr, so r.Sub is read.
		r(idWritesR, tRHolder+".Sub", "83"),
		r(idWritesR, tRHolder+".Key", "84"), // the index
		// 85: update only. 86: the matching r.N is suppressed, r.Step isn't.
		r(idWritesR, tRHolder+".Step", "86"),
		// 87: update only; append is a builtin, so no escape either.

		// addresses: & is an escape; no reads on struct-value hops up to the first pointer.
		e(idAddrs, tRHolder+".N", "91"),
		e(idAddrs, tRPart+".X", "92"), // nothing on Holder.Val
		e(idAddrs, tRPart+".X", "93"),
		r(idAddrs, tRHolder+".Ptr", "93"), // the pointer is loaded
		e(idAddrs, storePkg+".shared", "95"),

		// calls: a read unless Go takes the field's address for a pointer method.
		r(idCalls, tRHolder+".Ptr", "99"),
		e(idCalls, tRHolder+".Val", "100"),
		r(idCalls, tRHolder+".Val", "101"),
		r(idCalls, tRHolder+".H", "102"),
		r(idCalls, tRHolder+".OnErr", "103"),
		e(idCalls, tRHolder+".mu", "104"),
		// 105: r is a param, nothing.
		e(idCalls, storePkg+".shared", "106"),
		r(idCalls, storePkg+".OnReady", "107"),
		e(idCalls, tRHolder+".Val", "108"), // method value, address case
		r(idCalls, tRHolder+".Ptr", "109"),
		e(storePkg+".lock", tRLocked+".Mutex", "115"), // promoted through the embedded Mutex

		// outsides: repo consts are read; outside consts get nothing; outside vars and fields are kept.
		r(idOutsides, maxUsers, "119"),
		// 120: http.StatusOK, nothing.
		r(idOutsides, "os.Args", "121"),
		r(idOutsides, "net/http.Request.Method", "122"),
		r(idOutsides, "net/url.URL.Path", "122"),
		r(idOutsides, "net/http.Request.URL", "122"),
		// 123-124: a local const, nothing.

		// hands: a slice or map field passed, stored, sent or returned is read and escapes.
		r(idHands, tRHolder+".Items", "128"),
		e(idHands, tRHolder+".Items", "128"),
		r(idHands, tRHolder+".Users", "129"),
		e(idHands, tRHolder+".Users", "129"),
		r(idHands, tRHolder+".Items", "130"),
		e(idHands, tRHolder+".Items", "130"),
		r(idHands, tRHolder+".Items", "131"), // into a literal
		e(idHands, tRHolder+".Items", "131"),
		r(idHands, tRHolder+".Items", "132"),
		e(idHands, tRHolder+".Items", "132"),
		r(idHands, tRHolder+".Ptr", "133"),   // pointers don't escape
		r(idHands, tRHolder+".Items", "134"), // r.Items[1:] isn't the field itself
		r(idHands, tRHolder+".Users", "136"),
		e(idHands, tRHolder+".Users", "136"),

		// The generic field, like writes.
		r(storePkg+".bagLen", tRBag+".items", "139"),
	}

	got := make(map[string]int)
	for _, a := range g.Accesses {
		if (a.Kind == ReadAccess || a.Kind == EscapeAccess) && strings.HasPrefix(a.Pos, "store/reads.go:") {
			got[formatAccess(a)]++
		}
	}
	for _, w := range want {
		key := formatAccess(w)
		if got[key] == 0 {
			t.Errorf("missing access %s", key)
			continue
		}
		got[key]--
	}
	for key, n := range got {
		for range n {
			t.Errorf("unexpected access %s", key)
		}
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

func formatAccess(a Access) string {
	return fmt.Sprintf("%s %s %s at %s", a.Accessor, a.Kind, a.Target, a.Pos)
}

func formatCall(c FunctionCall) string {
	return fmt.Sprintf("%s -> %q at %s (%s) args=%v", c.Caller, c.Callee, c.Pos, c.CallKind, c.Args)
}
