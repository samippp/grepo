// Expected BuildCallGraph output for testdata/simple.

package graph

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
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
		readsFunc("sized", "157", []Param{{Name: "b", Type: "[100]byte"}}, []Param{{Name: "", Type: "[100]byte"}}),

		// The closures fixture's declared functions (store/closures.go).
		{
			ID: idStart, Name: "start", Package: storePkg,
			Receiver: &Param{Name: "r", Type: "*" + tCRunner},
			Pos:      "store/closures.go:15", Owner: tCRunner,
		},
		{
			ID: "(*" + tCRunner + ").serve", Name: "serve", Package: storePkg,
			Receiver: &Param{Name: "r", Type: "*" + tCRunner},
			Params:   []Param{{Name: "w", Type: "net/http.ResponseWriter"}, {Name: "req", Type: "*net/http.Request"}},
			Pos:      "store/closures.go:17", Owner: tCRunner,
		},
		{
			ID: storePkg + ".handle", Name: "handle", Package: storePkg,
			Params: []Param{{Name: "w", Type: "net/http.ResponseWriter"}, {Name: "req", Type: "*net/http.Request"}},
			Pos:    "store/closures.go:19",
		},
		{
			ID: storePkg + ".double", Name: "double", Package: storePkg,
			Params: []Param{{Name: "x", Type: "T"}}, Results: []Param{{Name: "", Type: "T"}},
			Pos: "store/closures.go:21",
		},
		{
			ID: storePkg + ".mapAll", Name: "mapAll", Package: storePkg,
			Params: []Param{{Name: "xs", Type: "[]T"}, {Name: "f", Type: "func(T) T"}},
			Pos:    "store/closures.go:23",
		},
		{
			ID: idClosures, Name: "closures", Package: storePkg,
			Receiver: &Param{Name: "r", Type: "*" + tCRunner},
			Params:   []Param{{Name: "xs", Type: "[]string"}},
			Pos:      "store/closures.go:29", Owner: tCRunner,
		},
		{
			ID: idNested, Name: "nested", Package: storePkg,
			Receiver: &Param{Name: "o", Type: "*" + tROuter},
			Pos:      "store/reads.go:147", Owner: tROuter,
		},
	}

	// Closures have their own test.
	var got []Function
	for _, f := range g.Functions {
		if f.Parent == "" {
			got = append(got, f)
		}
	}
	checkExact(t, "function", got, want, formatFunction)
}

func TestCallGraphClosures(t *testing.T) {
	g := buildFixtureCallGraph(t)

	// closure is a closure node: no receiver, owner or export, even inside methods.
	// The fixture's closures are in store, except main.go's.
	closure := func(parent string, n int, pos string, params, results []Param) Function {
		id := fmt.Sprintf("%s$%d", parent, n)
		pkg := storePkg
		if strings.HasPrefix(pos, "main.go:") {
			pkg = mainPkg
		}
		return Function{
			ID: id, Name: id[strings.LastIndex(id, ".")+1:], Package: pkg,
			Params: params, Results: results, Pos: pos, Parent: parent,
		}
	}
	at := func(line string) string { return "store/closures.go:" + line }
	c1 := idClosures + "$1"

	want := []Function{
		// Existing fixture code.
		closure(idMain, 1, "main.go:25", nil, nil),
		closure(mainPkg+".spawn", 1, "main.go:107", nil, nil), // holds the local type job
		closure(storePkg+".OnReady", 1, "store/reads.go:52", nil, nil),
		closure(idValues, 1, "store/reads.go:76", nil, nil),
		closure(storePkg+".sized", 1, "store/reads.go:159", []Param{{Name: "d", Type: "[100]byte"}}, nil),

		// In a generic function.
		closure(storePkg+".mapAll", 1, at("25"), nil, nil),
		// In a method, numbered in source order; nested ones under their parent.
		closure(idClosures, 1, at("30"), nil, nil),
		closure(c1, 1, at("31"), nil, nil),
		closure(idClosures, 2, at("33"), nil, nil),
		closure(idClosures, 3, at("37"), nil, nil),
		closure(idClosures, 4, at("38"), nil, nil),
		closure(idClosures, 5, at("39"), []Param{{Name: "i", Type: "int"}, {Name: "j", Type: "int"}}, []Param{{Name: "", Type: "bool"}}),
		closure(idClosures, 6, at("43"), nil, nil),
		closure(idClosures, 7, at("45"), nil, []Param{{Name: "", Type: "error"}}),
		// Package level: under the var each value initializes.
		closure(storePkg+".hooks", 1, at("49"), nil, nil),
		closure(storePkg+".fallback", 1, at("49"), nil, nil),
		closure(storePkg+".nested", 1, at("53"), nil, nil),
		closure(storePkg+".nested$1", 1, at("54"), nil, nil),
	}
	// A blank var has no node: its closure is the package's, named after _.
	blank := closure(storePkg+"._", 1, at("51"), nil, []Param{{Name: "", Type: "int"}})
	blank.Parent = storePkg
	want = append(want, blank)

	var got []Function
	for _, f := range g.Functions {
		if f.Parent != "" {
			got = append(got, f)
		}
	}
	checkExact(t, "closure", got, want, formatFunction)
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
		// Inside a closure: the closure is its parent.
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
		{
			ID: tCRunner, Name: "Runner", Package: storePkg, Kind: StructType, Exported: true,
			Pos: "store/closures.go:10",
			Fields: []Field{
				{ID: tCRunner + ".OnDone", Name: "OnDone", Type: "func()", Pos: "store/closures.go:11"},
				{ID: tCRunner + ".Run", Name: "Run", Type: "func() error", Pos: "store/closures.go:12"},
			},
		},
		{
			ID: tROuter, Name: "Outer", Package: storePkg, Kind: StructType, Exported: true,
			Pos: "store/reads.go:142",
			Fields: []Field{
				{ID: tROuter + ".In", Name: "In", Type: tRHolder, Pos: "store/reads.go:143"},
				{ID: tROuter + ".L", Name: "L", Type: tRLocked, Pos: "store/reads.go:144"},
			},
		},
	}

	checkExact(t, "type", g.Types, want, func(ty Type) string { return fmt.Sprintf("%+v", ty) })
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

	checkExact(t, "implementation", g.Implementations, want, func(im Implementation) string { return fmt.Sprintf("%+v", im) })
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
		{ID: storePkg + ".table", Name: "table", Package: storePkg, Type: "[100]int", Pos: "store/reads.go:164"},
		{ID: storePkg + ".hooks", Name: "hooks", Package: storePkg, Type: "func()", Pos: "store/closures.go:49"},
		{ID: storePkg + ".fallback", Name: "fallback", Package: storePkg, Type: "func()", Pos: "store/closures.go:49"},
		{ID: storePkg + ".nested", Name: "nested", Package: storePkg, Type: "func()", Pos: "store/closures.go:53"},
	}

	checkExact(t, "variable", g.Variables, want, func(v Variable) string { return fmt.Sprintf("%+v", v) })
}

func TestCallGraphCalls(t *testing.T) {
	g := buildFixtureCallGraph(t)

	rat := func(line string) string { return "store/reads.go:" + line }
	cat := func(line string) string { return "store/closures.go:" + line }

	want := []FunctionCall{
		// store.New(): function in another package.
		{Caller: idMain, Callee: idNew, Pos: "main.go:16", CallKind: StaticCall},
		// s.Get(1) where s is *Store: method on a concrete type.
		{Caller: idMain, Callee: idGet, Pos: "main.go:17", CallKind: StaticCall,
			Args: []Arg{{Expr: "1", Type: "int"}}},
		// g.Get(2) where g is store.Getter: points at the interface method.
		{Caller: idMain, Callee: "(" + storePkg + ".Getter).Get", Pos: "main.go:20", CallKind: InterfaceCall,
			Args: []Arg{{Expr: "2", Type: "int"}}},
		// f(u.Name) where f := greet: a function value, target unknown.
		{Caller: idMain, Callee: "", Pos: "main.go:23", CallKind: DynamicCall,
			Args: []Arg{{Expr: "u.Name", Type: "string"}}},
		// func(){...}(): a static call to the closure, and greet("closure") inside it belongs to it.
		{Caller: idMain, Callee: idMain + "$1", Pos: "main.go:25", CallKind: StaticCall},
		{Caller: idMain + "$1", Callee: idGreet, Pos: "main.go:26", CallKind: StaticCall,
			Args: []Arg{{Expr: `"closure"`, Type: "string"}}},
		// Nothing for len(u.Name) (built-in) or int64(n) (conversion).
		// fmt.Println(u): outside the repo, but kept.
		{Caller: idMain, Callee: "fmt.Println", Pos: "main.go:32", CallKind: StaticCall,
			Args: []Arg{{Expr: "u", Type: "example.com/simple/model.User"}}},
		{Caller: idGreet, Callee: "fmt.Println", Pos: "main.go:36", CallKind: StaticCall,
			Args: []Arg{{Expr: `"hello"`, Type: "string"}, {Expr: "name", Type: "string"}}},
		// s.mu.Lock(): method reached through a struct field.
		{Caller: idGet, Callee: "(*sync.Mutex).Lock", Pos: "store/store.go:24", CallKind: StaticCall},
		// defer s.mu.Unlock(): deferred calls count too.
		{Caller: idGet, Callee: "(*sync.Mutex).Unlock", Pos: "store/store.go:25", CallKind: StaticCall},
		// first[int](xs): explicit type arg (IndexExpr), target known (#4).
		{Caller: idGenerics, Callee: idFirst, Pos: "main.go:46", CallKind: StaticCall,
			Args: []Arg{{Expr: "xs", Type: "[]int"}}},
		// pair[string, int](...): two type args (IndexListExpr) (#4).
		{Caller: idGenerics, Callee: idPair, Pos: "main.go:47", CallKind: StaticCall,
			Args: []Arg{{Expr: `"a"`, Type: "string"}, {Expr: "1", Type: "int"}}},
		// handlers[0](...): also an IndexExpr, but a func value, so dynamic.
		{Caller: idGenerics, Callee: "", Pos: "main.go:50", CallKind: DynamicCall,
			Args: []Arg{{Expr: `"indexed"`, Type: "string"}}},
		// l.Push(1) on List[int]: callee must be the declared List[T] method (#5).
		{Caller: idUseList, Callee: idPush, Pos: "main.go:60", CallKind: StaticCall,
			Args: []Arg{{Expr: "1", Type: "int"}}},
		// Calls from each init are attributed to that init (#6).
		{Caller: idInit1, Callee: idGreet, Pos: "main.go:64", CallKind: StaticCall,
			Args: []Arg{{Expr: `"init 1"`, Type: "string"}}},
		{Caller: idInit2, Callee: idGreet, Pos: "main.go:66", CallKind: StaticCall,
			Args: []Arg{{Expr: `"init 2"`, Type: "string"}}},
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
		{Caller: idValues, Callee: storePkg + ".use", Pos: rat("64"), CallKind: StaticCall, Args: []Arg{{Expr: "r.N", Type: "int"}}},
		{Caller: idValues, Callee: storePkg + ".two", Pos: rat("75"), CallKind: StaticCall, Args: []Arg{{Expr: "r.N", Type: "int"}, {Expr: "r.N", Type: "int"}}},
		{Caller: idValues, Callee: idValues + "$1", Pos: rat("76"), CallKind: StaticCall}, // the func literal called where it's written
		{Caller: idCalls, Callee: "(*" + tRPart + ").Bump", Pos: rat("99"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "(*" + tRPart + ").Bump", Pos: rat("100"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "(" + tRPart + ").Value", Pos: rat("101"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "(net/http.Handler).ServeHTTP", Pos: rat("102"), CallKind: InterfaceCall,
			Args: []Arg{{Expr: "w", Type: "net/http.ResponseWriter"}, {Expr: "req", Type: "*net/http.Request"}}},
		{Caller: idCalls, Callee: "", Pos: rat("103"), CallKind: DynamicCall, Args: []Arg{{Expr: "nil", Type: "untyped nil"}}},
		{Caller: idCalls, Callee: "(*sync.Mutex).Lock", Pos: rat("104"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "(*" + tRHolder + ").Touch", Pos: rat("105"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "(*" + tRHolder + ").Touch", Pos: rat("106"), CallKind: StaticCall},
		{Caller: idCalls, Callee: "", Pos: rat("107"), CallKind: DynamicCall},
		{Caller: idCalls, Callee: "", Pos: rat("110"), CallKind: DynamicCall},
		{Caller: idCalls, Callee: "", Pos: rat("111"), CallKind: DynamicCall},
		{Caller: storePkg + ".lock", Callee: "(*sync.Mutex).Lock", Pos: rat("115"), CallKind: StaticCall},
		{Caller: idHands, Callee: storePkg + ".sum", Pos: rat("128"), CallKind: StaticCall, Args: []Arg{{Expr: "r.Items", Type: "[]int"}}},
		{Caller: idHands, Callee: storePkg + ".use", Pos: rat("128"), CallKind: StaticCall, Args: []Arg{{Expr: "sum(r.Items)", Type: "int"}}},
		{Caller: idHands, Callee: storePkg + ".keep", Pos: rat("133"), CallKind: StaticCall, Args: []Arg{{Expr: "r.Ptr", Type: "*" + tRPart}}},
		{Caller: idHands, Callee: storePkg + ".sum", Pos: rat("134"), CallKind: StaticCall, Args: []Arg{{Expr: "r.Items[1:]", Type: "[]int"}}},
		{Caller: idHands, Callee: storePkg + ".use", Pos: rat("134"), CallKind: StaticCall, Args: []Arg{{Expr: "sum(r.Items[1:])", Type: "int"}}},
		{Caller: idNested, Callee: "(*" + tRPart + ").Bump", Pos: rat("149"), CallKind: StaticCall, Args: []Arg{}},
		{Caller: idNested, Callee: "(*sync.Mutex).Lock", Pos: rat("150"), CallKind: StaticCall, Args: []Arg{}},

		// go func(){...}() in spawn.
		{Caller: mainPkg + ".spawn", Callee: mainPkg + ".spawn$1", Pos: "main.go:107", CallKind: StaticCall, Stmt: "go"},

		// The closures fixture (store/closures.go).
		{Caller: storePkg + ".mapAll", Callee: storePkg + ".mapAll$1", Pos: cat("25"), CallKind: StaticCall},
		{Caller: storePkg + ".mapAll$1", Callee: "", Pos: cat("25"), CallKind: DynamicCall, Args: []Arg{{Expr: "xs[i]", Type: "T"}}},
		{Caller: idClosures, Callee: idClosures + "$1", Pos: cat("30"), CallKind: StaticCall},
		{Caller: idClosures + "$1", Callee: idClosures + "$1$1", Pos: cat("31"), CallKind: StaticCall},
		{Caller: idClosures, Callee: "", Pos: cat("34"), CallKind: DynamicCall}, // inc(): through a local, not guessed
		{Caller: idClosures, Callee: idStart, Pos: cat("35"), CallKind: StaticCall, Stmt: "go"},
		{Caller: idClosures, Callee: idStart, Pos: cat("36"), CallKind: StaticCall, Stmt: "defer"},
		{Caller: idClosures, Callee: idClosures + "$3", Pos: cat("37"), CallKind: StaticCall, Stmt: "go"},
		{Caller: idClosures + "$3", Callee: idStart, Pos: cat("37"), CallKind: StaticCall},
		{Caller: idClosures, Callee: idClosures + "$4", Pos: cat("38"), CallKind: StaticCall, Stmt: "defer"},
		// Func: the function an argument is: a closure, a declared function, a method value, a generic instance.
		{Caller: idClosures, Callee: "sort.Slice", Pos: cat("39"), CallKind: StaticCall, Args: []Arg{
			{Expr: "xs", Type: "[]string"},
			{Expr: "(func(i, j int) bool literal)", Type: "func(i int, j int) bool", Func: idClosures + "$5"}}},
		{Caller: idClosures, Callee: "net/http.HandleFunc", Pos: cat("40"), CallKind: StaticCall, Args: []Arg{
			{Expr: `"/a"`, Type: "string"},
			{Expr: "handle", Type: "func(w net/http.ResponseWriter, req *net/http.Request)", Func: storePkg + ".handle"}}},
		{Caller: idClosures, Callee: "net/http.HandleFunc", Pos: cat("41"), CallKind: StaticCall, Args: []Arg{
			{Expr: `"/b"`, Type: "string"},
			{Expr: "r.serve", Type: "func(w net/http.ResponseWriter, req *net/http.Request)", Func: "(*" + tCRunner + ").serve"}}},
		{Caller: idClosures, Callee: storePkg + ".mapAll", Pos: cat("42"), CallKind: StaticCall, Args: []Arg{
			{Expr: "xs", Type: "[]string"},
			{Expr: "double[string]", Type: "func(x string) string", Func: storePkg + ".double"}}},
		{Caller: idClosures, Callee: "", Pos: cat("46"), CallKind: DynamicCall}, // r.OnDone(): Through, not a target
		// Package level: the package calls the blank var's closure; nested's closure calls its own.
		{Caller: storePkg, Callee: storePkg + "._$1", Pos: cat("51"), CallKind: StaticCall},
		{Caller: storePkg + ".nested$1", Callee: storePkg + ".nested$1$1", Pos: cat("54"), CallKind: StaticCall},
	}

	checkExact(t, "call", g.Calls, want, formatCall)
}

func TestCallGraphAccesses(t *testing.T) {
	g := buildFixtureCallGraph(t)

	idWrites := "(*" + tWBox + ").Writes"
	w := func(accessor, target, pos string, kind AccessKind) Access {
		return Access{Accessor: accessor, Target: target, Pos: pos, Kind: kind}
	}
	wf := func(accessor, target, pos string, kind AccessKind, fn string) Access {
		a := w(accessor, target, pos, kind)
		a.Func = fn
		return a
	}
	at := func(line string) string { return "store/writes.go:" + line }
	rat := func(line string) string { return "store/reads.go:" + line }
	cat := func(line string) string { return "store/closures.go:" + line }
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

		// Writes in the closures fixture (store/closures.go). Func: the function the value is.
		wf(idClosures, tCRunner+".OnDone", cat("43"), AssignAccess, idClosures+"$6"),
		wf(idClosures, tCRunner+".OnDone", cat("44"), AssignAccess, idStart), // a method value
		wf(idClosures, tCRunner+".Run", cat("45"), ConstructAccess, idClosures+"$7"),
		wf(storePkg, storePkg+".hooks", cat("49"), ConstructAccess, storePkg+".hooks$1"),
		wf(storePkg, storePkg+".fallback", cat("49"), ConstructAccess, storePkg+".fallback$1"),
		wf(storePkg, storePkg+".nested", cat("53"), ConstructAccess, storePkg+".nested$1"),
		// 51: a blank var has no node, so no construct edge.

		// Writes in the reads fixture (store/reads.go).
		w("(*"+tRPart+").Bump", tRPart+".X", rat("13"), UpdateAccess),
		w(storePkg, storePkg+".limit", rat("46"), ConstructAccess),
		wf(storePkg, storePkg+".OnReady", rat("52"), ConstructAccess, storePkg+".OnReady$1"),
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

	// Reads and escapes have their own test.
	var got []Access
	for _, a := range g.Accesses {
		if a.Kind != ReadAccess && a.Kind != EscapeAccess {
			got = append(got, a)
		}
	}
	checkExact(t, "access", got, want, formatAccess)
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
		{idNested, "(*" + tRPart + ").Bump", rat("149")}:       tRHolder + ".Val",     // the innermost field
		{idNested, "(*sync.Mutex).Lock", rat("150")}:           tRLocked + ".Mutex",
		{idClosures, "", "store/closures.go:46"}:               tCRunner + ".OnDone", // a func field
		// None for r.Touch() (a param) or f() and g() (method values in locals).
	}

	var got []FunctionCall
	for _, c := range g.Calls {
		if c.Through != "" {
			got = append(got, c)
		}
	}
	var wantCalls []FunctionCall
	for k, through := range want {
		wantCalls = append(wantCalls, FunctionCall{Caller: k.caller, Callee: k.callee, Pos: k.pos, Through: through})
	}
	checkExact(t, "through", got, wantCalls, func(c FunctionCall) string {
		return fmt.Sprintf("%s -> %q at %s: %s", c.Caller, c.Callee, c.Pos, c.Through)
	})
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
		r(idValues, tRHolder+".N", "75"),         // one edge per site: two on this line
		r(idValues+"$1", tRHolder+".Step", "76"), // in a closure: belongs to it

		// writes: no read where the path gets a write edge.
		// 81, 82: nothing. 83: the write stops at Sub.Ptr, so r.Sub is read.
		r(idWritesR, tRHolder+".Sub", "83"),
		r(idWritesR, tRHolder+".Key", "84"), // the index
		// 85: update only. 86: the matching r.N is suppressed, r.Step isn't.
		r(idWritesR, tRHolder+".Step", "86"),
		// 87: update only; append is a builtin, so no escape either.

		// addresses: & is an escape, and so is every struct-value hop up to the
		// first pointer (no reads on them); the pointer and hops before it are read.
		e(idAddrs, tRHolder+".N", "91"),
		e(idAddrs, tRPart+".X", "92"),
		e(idAddrs, tRHolder+".Val", "92"), // Val's memory holds X
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

		// nested: escapes reach every enclosing struct value, like writes' content.
		e(idNested, tRPart+".X", "148"),
		e(idNested, tRHolder+".Val", "148"),
		e(idNested, tROuter+".In", "148"),
		e(idNested, tRHolder+".Val", "149"), // pointer method on a nested value
		e(idNested, tROuter+".In", "149"),
		e(idNested, tRLocked+".Mutex", "150"), // promoted, on a value field
		e(idNested, tROuter+".L", "150"),
		e(idNested, tRBase+".ID", "151"), // embedded hops too
		e(idNested, tRHolder+".Base", "151"),
		e(idNested, tROuter+".In", "151"),
		e(idNested, tRPart+".X", "152"),
		e(idNested, tRHolder+".Val", "152"),
		e(idNested, storePkg+".shared", "152"), // the package var holding it
		e(idNested, tRPart+".X", "153"),
		r(idNested, tRHolder+".Ptr", "153"), // stops at the pointer: it and the hops before it are read
		r(idNested, tROuter+".In", "153"),

		// Consts in types: the param and result types, a local var's type, a
		// closure's signature, a literal's type; a package var's type, by the package.
		r(storePkg+".sized", maxUsers, "157"),
		r(storePkg+".sized", maxUsers, "157"),
		r(storePkg+".sized", maxUsers, "158"),
		r(storePkg+".sized$1", maxUsers, "159"), // the closure's own signature
		r(storePkg+".sized", maxUsers, "161"),
		r(storePkg, maxUsers, "164"),
	}

	var got []Access
	for _, a := range g.Accesses {
		if (a.Kind == ReadAccess || a.Kind == EscapeAccess) && strings.HasPrefix(a.Pos, "store/reads.go:") {
			got = append(got, a)
		}
	}
	checkExact(t, "access", got, want, formatAccess)
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

func formatFunction(f Function) string {
	recv := "<nil>"
	if f.Receiver != nil {
		recv = fmt.Sprint(*f.Receiver)
	}
	return fmt.Sprintf("%s {name=%s pkg=%s recv=%s params=%v results=%v exported=%v pos=%s owner=%s parent=%s}",
		f.ID, f.Name, f.Package, recv, f.Params, f.Results, f.Exported, f.Pos, f.Owner, f.Parent)
}

func formatAccess(a Access) string {
	s := fmt.Sprintf("%s %s %s at %s", a.Accessor, a.Kind, a.Target, a.Pos)
	if a.Func != "" {
		s += " func=" + a.Func
	}
	return s
}

func formatCall(c FunctionCall) string {
	s := fmt.Sprintf("%s -> %q at %s (%s) args=%v", c.Caller, c.Callee, c.Pos, c.CallKind, c.Args)
	if c.Stmt != "" {
		s += " stmt=" + c.Stmt
	}
	return s
}
