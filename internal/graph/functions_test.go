package graph

import (
	"fmt"
	"strings"
	"testing"
)

func TestCallGraphFunctions(t *testing.T) {
	g := buildFixtureCallGraph(t)

	// Holder's unexported methods and the helpers in store/reads.go.
	holderMethod := func(name, line string, params, results []Param) Function {
		return Function{
			ID: "(*" + tRHolder + ")." + name, Name: name, Package: storePkg, Receiver: recv("r", "*"+tRHolder),
			Params: params, Results: results, Pos: rat(line), Owner: tRHolder,
		}
	}
	readsFunc := func(name, line string, params, results []Param) Function {
		return Function{ID: storePkg + "." + name, Name: name, Package: storePkg, Params: params, Results: results, Pos: rat(line)}
	}
	ints := ps("", "int")
	handler := ps("w", "net/http.ResponseWriter", "req", "*net/http.Request")

	want := []Function{
		{ID: idMain, Name: "main", Package: mainPkg, Pos: "main.go:15"},
		{ID: idGreet, Name: "greet", Package: mainPkg, Params: ps("name", "string"), Pos: "main.go:35"},
		{ID: idNew, Name: "New", Package: storePkg, Results: ps("", "*"+tStore), Exported: true, Pos: "store/store.go:21"},
		{ID: idGet, Name: "Get", Package: storePkg, Receiver: recv("s", "*"+tStore), Params: ps("id", "int"),
			Results: ps("", tUser), Exported: true, Pos: "store/store.go:23", Owner: tStore},
		{ID: idFirst, Name: "first", Package: mainPkg, Params: ps("xs", "[]T"), Results: ps("", "T"), Pos: "main.go:39"},
		{ID: idPair, Name: "pair", Package: mainPkg, Params: ps("k", "K", "v", "V"), Pos: "main.go:41"},
		{ID: idGenerics, Name: "generics", Package: mainPkg, Pos: "main.go:44"},
		{ID: idPush, Name: "Push", Package: mainPkg, Receiver: recv("l", "*"+tList), Params: ps("v", "T"),
			Exported: true, Pos: "main.go:56", Owner: tList},
		{ID: idUseList, Name: "useList", Package: mainPkg, Pos: "main.go:58"},
		// Several inits per package: file + position makes each ID unique (#6).
		{ID: idInit1, Name: "init", Package: mainPkg, Pos: "main.go:64"},
		{ID: idInit2, Name: "init", Package: mainPkg, Pos: "main.go:66"},
		{ID: idStoreInit, Name: "init", Package: storePkg, Pos: "store/store.go:29"},
		// A method named init is an ordinary method: normal ID.
		{ID: idListInit, Name: "init", Package: mainPkg, Receiver: recv("l", "*"+tList), Pos: "main.go:68", Owner: tList},
		// Value receiver on a non-struct type.
		{ID: "(" + tUserID + ").String", Name: "String", Package: modelPkg, Receiver: recv("u", tUserID),
			Results: ps("", "string"), Exported: true, Pos: "model/kinds.go:10", Owner: tUserID},
		// Interface methods: explicit ones only, owned by their interface, no receiver.
		{ID: "(" + tGetter + ").Get", Name: "Get", Package: storePkg, Params: ps("id", "int"), Results: ps("", tUser),
			Exported: true, Pos: "store/store.go:13", Owner: tGetter},
		{ID: "(" + tReadNamer + ").Name", Name: "Name", Package: modelPkg, Results: ps("", "string"),
			Exported: true, Pos: "model/kinds.go:15", Owner: tReadNamer},
		{ID: storePkg + ".load", Name: "load", Package: storePkg, Results: ps("", "int", "", "error"), Pos: "store/vars.go:13"},
		{ID: storePkg + ".register", Name: "register", Package: storePkg, Results: ps("", "bool"), Pos: "store/vars.go:19"},
		{ID: "(" + tCache + ").Get", Name: "Get", Package: storePkg, Receiver: recv("c", tCache), Params: ps("id", "int"),
			Results: ps("", tUser), Exported: true, Pos: "store/cache.go:9", Owner: tCache},
		{ID: "(" + tNamer + ").Name", Name: "Name", Package: modelPkg, Results: ps("", "string"),
			Exported: true, Pos: "model/kinds.go:24", Owner: tNamer},
		// Unnamed receiver: func (Label) Name().
		{ID: "(" + tLabel + ").Name", Name: "Name", Package: modelPkg, Receiver: recv("", tLabel), Results: ps("", "string"),
			Exported: true, Pos: "model/kinds.go:28", Owner: tLabel},
		// Method IDs use the owner's type ID. A generic interface method has no
		// constraint in its ID (not Source[T any]).
		{ID: "(" + tSource + ").Next", Name: "Next", Package: mainPkg, Results: ps("", "T"),
			Exported: true, Pos: "main.go:71", Owner: tSource},
		{ID: mainPkg + ".drain", Name: "drain", Package: mainPkg, Params: ps("s", mainPkg+".Source[int]"), Pos: "main.go:73"},
		// Receiver written as List[E]: the ID still uses the declaration's List[T].
		{ID: "(*" + tList + ").Len", Name: "Len", Package: mainPkg, Receiver: recv("l", "*"+mainPkg+".List[E]"),
			Results: ints, Exported: true, Pos: "main.go:75", Owner: tList},
		// Declared through the alias S: the ID and owner are Store's.
		{ID: "(*" + tStore + ").Reset", Name: "Reset", Package: storePkg, Receiver: recv("s", "*"+tS),
			Exported: true, Pos: "store/alias.go:6", Owner: tStore},
		{ID: mainPkg + ".describe", Name: "describe", Package: mainPkg, Params: ps("err", "error"), Results: ps("", "string"), Pos: "main.go:78"},
		{ID: mainPkg + ".parse", Name: "parse", Package: mainPkg, Pos: "main.go:83"},
		{ID: mainPkg + ".save", Name: "save", Package: mainPkg, Params: ps("ok", "bool"), Pos: "main.go:87"},
		{ID: mainPkg + ".announce", Name: "announce", Package: mainPkg, Params: ps("v", "any"), Pos: "main.go:98"},
		// A local interface's method: owned by the local type, ID built from its ID.
		{ID: "(" + tLocalNamer + ").Name", Name: "Name", Package: mainPkg, Results: ps("", "string"),
			Exported: true, Pos: "main.go:99", Owner: tLocalNamer},
		{ID: mainPkg + ".spawn", Name: "spawn", Package: mainPkg, Pos: "main.go:106"},
		{ID: mainPkg + ".wrap", Name: "wrap", Package: mainPkg, Params: ps("v", "T"), Pos: "main.go:113"},

		{ID: "(*" + tWBox + ").Writes", Name: "Writes", Package: storePkg, Receiver: recv("b", "*"+tWBox),
			Params: ps("o", "*"+tWBox, "src", "[]byte"), Exported: true, Pos: wat("30"), Owner: tWBox},
		{ID: storePkg + ".locals", Name: "locals", Package: storePkg, Pos: wat("52")},
		{ID: storePkg + ".build", Name: "build", Package: storePkg, Results: ps("", "*"+tWBox), Pos: wat("60")},
		{ID: storePkg + ".points", Name: "points", Package: storePkg, Results: ps("", "[]"+tWInner), Pos: wat("64")},
		{ID: storePkg + ".outside", Name: "outside", Package: storePkg, Results: ps("", "*net/http.Server"), Pos: wat("71")},
		{ID: "(*" + tWBox + ").ranges", Name: "ranges", Package: storePkg, Receiver: recv("b", "*"+tWBox),
			Params: ps("xs", "[]int"), Pos: wat("77"), Owner: tWBox},
		{ID: "(*" + tWBox + ").derefs", Name: "derefs", Package: storePkg, Receiver: recv("b", "*"+tWBox), Pos: wat("83"), Owner: tWBox},
		{ID: storePkg + ".promoted", Name: "promoted", Package: storePkg, Pos: wat("93")},

		{ID: "(*" + tRPart + ").Bump", Name: "Bump", Package: storePkg, Receiver: recv("p", "*"+tRPart),
			Exported: true, Pos: rat("13"), Owner: tRPart},
		{ID: "(" + tRPart + ").Value", Name: "Value", Package: storePkg, Receiver: recv("p", tRPart), Results: ints,
			Exported: true, Pos: rat("15"), Owner: tRPart},
		{ID: "(*" + tRHolder + ").Touch", Name: "Touch", Package: storePkg, Receiver: recv("r", "*"+tRHolder),
			Exported: true, Pos: rat("36"), Owner: tRHolder},
		readsFunc("use", "54", ps("n", "int"), nil),
		readsFunc("two", "56", ps("a", "int", "b", "int"), nil),
		readsFunc("sum", "58", ps("xs", "[]int"), ints),
		readsFunc("keep", "60", ps("p", "*"+tRPart), nil),
		holderMethod("values", "62", ps("xs", "[]int"), ints),
		holderMethod("writes", "80", ps("v", "string"), nil),
		holderMethod("addresses", "90", nil, nil),
		holderMethod("calls", "98", handler, nil),
		readsFunc("lock", "114", ps("l", "*"+tRLocked), nil),
		holderMethod("outsides", "118", ps("req", "*net/http.Request"), ints),
		holderMethod("hands", "127", ps("other", "*"+tRHolder, "ch", "chan []int"), ps("", "map[int]string")),
		readsFunc("bagLen", "139", ps("b", "*"+storePkg+".Bag[int]"), ints),
		readsFunc("sized", "157", ps("b", "[100]byte"), ps("", "[100]byte")),
		{ID: idNested, Name: "nested", Package: storePkg, Receiver: recv("o", "*"+tROuter), Pos: rat("147"), Owner: tROuter},

		// The closures fixture's declared functions (store/closures.go).
		{ID: idStart, Name: "start", Package: storePkg, Receiver: recv("r", "*"+tCRunner), Pos: cat("15"), Owner: tCRunner},
		{ID: "(*" + tCRunner + ").serve", Name: "serve", Package: storePkg, Receiver: recv("r", "*"+tCRunner),
			Params: handler, Pos: cat("17"), Owner: tCRunner},
		{ID: storePkg + ".handle", Name: "handle", Package: storePkg, Params: handler, Pos: cat("19")},
		{ID: storePkg + ".double", Name: "double", Package: storePkg, Params: ps("x", "T"), Results: ps("", "T"), Pos: cat("21")},
		{ID: storePkg + ".mapAll", Name: "mapAll", Package: storePkg, Params: ps("xs", "[]T", "f", "func(T) T"), Pos: cat("23")},
		{ID: idClosures, Name: "closures", Package: storePkg, Receiver: recv("r", "*"+tCRunner),
			Params: ps("xs", "[]string"), Pos: cat("29"), Owner: tCRunner},
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
	c1 := idClosures + "$1"

	want := []Function{
		// Existing fixture code.
		closure(idMain, 1, "main.go:25", nil, nil),
		closure(mainPkg+".spawn", 1, "main.go:107", nil, nil), // holds the local type job
		closure(storePkg+".OnReady", 1, rat("52"), nil, nil),
		closure(idValues, 1, rat("76"), nil, nil),
		closure(storePkg+".sized", 1, rat("159"), ps("d", "[100]byte"), nil),

		// In a generic function.
		closure(storePkg+".mapAll", 1, cat("25"), nil, nil),
		// In a method, numbered in source order; nested ones under their parent.
		closure(idClosures, 1, cat("30"), nil, nil),
		closure(c1, 1, cat("31"), nil, nil),
		closure(idClosures, 2, cat("33"), nil, nil),
		closure(idClosures, 3, cat("37"), nil, nil),
		closure(idClosures, 4, cat("38"), nil, nil),
		closure(idClosures, 5, cat("39"), ps("i", "int", "j", "int"), ps("", "bool")),
		closure(idClosures, 6, cat("43"), nil, nil),
		closure(idClosures, 7, cat("45"), nil, ps("", "error")),
		// Package level: under the var each value initializes.
		closure(storePkg+".hooks", 1, cat("49"), nil, nil),
		closure(storePkg+".fallback", 1, cat("49"), nil, nil),
		closure(storePkg+".nested", 1, cat("53"), nil, nil),
		closure(storePkg+".nested$1", 1, cat("54"), nil, nil),
	}
	// A blank var has no node: its closure is the package's, named after _.
	blank := closure(storePkg+"._", 1, cat("51"), nil, ps("", "int"))
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

// ps builds params from name, type pairs: ps("", "int") is one unnamed int.
func ps(nameTypes ...string) []Param {
	var out []Param
	for i := 0; i+1 < len(nameTypes); i += 2 {
		out = append(out, Param{Name: nameTypes[i], Type: nameTypes[i+1]})
	}
	return out
}

func recv(name, typ string) *Param { return &Param{Name: name, Type: typ} }

func formatFunction(f Function) string {
	recv := "<nil>"
	if f.Receiver != nil {
		recv = fmt.Sprint(*f.Receiver)
	}
	return fmt.Sprintf("%s {name=%s pkg=%s recv=%s params=%v results=%v exported=%v pos=%s owner=%s parent=%s}",
		f.ID, f.Name, f.Package, recv, f.Params, f.Results, f.Exported, f.Pos, f.Owner, f.Parent)
}
