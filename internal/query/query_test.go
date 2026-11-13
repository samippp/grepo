package query

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/samippp/grepo/internal/graph"
)

const (
	app      = "example.com/app"
	store    = app + "/store"
	cache    = app + "/cache"
	tStore   = store + ".Store"
	tCache   = store + ".Cache"
	tGetter  = store + ".Getter"
	tList    = store + ".List[T]"
	idGet    = "(*" + tStore + ").Get"
	idWrite  = "(*" + tStore + ").Write"
	idIGet   = "(" + tGetter + ").Get"
	idHandle = app + ".handle"
	idMain   = app + ".main"
)

// testIndex is a small repo: main calls Store.Get directly, from a closure
// and through Getter, Store.Write through io.Writer (from outside the repo),
// passes handle to http.HandleFunc and stores Store.Get in a field.
func testIndex() *Index {
	pg := &graph.PackageGraph{Packages: []graph.Package{
		{Path: app, Name: "main"}, {Path: store, Name: "store"}, {Path: cache, Name: "cache"},
	}}
	cg := &graph.CallGraph{
		Functions: []graph.Function{
			{ID: idGet, Name: "Get", Package: store, Owner: tStore},
			{ID: "(" + tCache + ").Get", Name: "Get", Package: store, Owner: tCache},
			{ID: idIGet, Name: "Get", Package: store, Owner: tGetter},
			{ID: store + ".Get", Name: "Get", Package: store},
			{ID: idWrite, Name: "Write", Package: store, Owner: tStore},
			{ID: "(*" + tList + ").Push", Name: "Push", Package: store, Owner: tList},
			{ID: store + ".Load", Name: "Load", Package: store},
			{ID: cache + ".Load", Name: "Load", Package: cache},
			{ID: idMain, Name: "main", Package: app},
			{ID: idMain + "$1", Name: "main$1", Package: app, Parent: idMain},
			{ID: idHandle, Name: "handle", Package: app},
			// A method of a local type: by ID only.
			{ID: "(" + app + ".run.local#1).Get", Name: "Get", Package: app, Owner: app + ".run.local#1"},
		},
		Calls: []graph.FunctionCall{
			{Caller: idMain, Callee: idGet, Pos: "main.go:17", CallKind: graph.StaticCall},
			{Caller: idMain + "$1", Callee: idGet, Pos: "main.go:9", CallKind: graph.StaticCall},
			{Caller: idMain, Callee: idIGet, Pos: "main.go:20", CallKind: graph.InterfaceCall},
			{Caller: idMain, Callee: "(io.Writer).Write", Pos: "main.go:30", CallKind: graph.InterfaceCall},
			{Caller: idMain, Callee: "net/http.HandleFunc", Pos: "main.go:40", CallKind: graph.StaticCall,
				Args: []graph.Arg{{Expr: `"/"`, Type: "string"}, {Expr: "handle", Func: idHandle}}},
			// Outside the repo: no nodes.
			{Caller: idMain, Callee: "os.Exit", Pos: "main.go:60", CallKind: graph.StaticCall},
			{Caller: idHandle, Callee: "(net/http.Header).Get", Pos: "main.go:70", CallKind: graph.StaticCall},
			{Caller: idHandle, Callee: "(*gopkg.in/yaml.v3.Node).Decode", Pos: "main.go:71", CallKind: graph.StaticCall},
		},
		Accesses: []graph.Access{
			{Accessor: idMain, Target: tStore + ".onGet", Pos: "main.go:50", Kind: graph.AssignAccess, Func: idGet},
		},
		Implementations: []graph.Implementation{
			{Type: tStore, Interface: tGetter, PointerOnly: true},
			{Type: tStore, Interface: "io.Writer", PointerOnly: true},
			{Type: tCache, Interface: tGetter},
		},
	}
	return New(pg, cg)
}

func TestFunc(t *testing.T) {
	ix := testIndex()
	for _, tc := range []struct{ name, prefer, want string }{
		{name: idGet, want: idGet}, // an ID
		{name: idMain + "$1", want: idMain + "$1"},
		{name: "Store.Get", want: idGet},
		{name: "(*Store).Get", want: idGet},
		{name: "*Store.Get", want: idGet},
		{name: "store.Store.Get", want: idGet},
		{name: "app/store.Store.Get", want: idGet},
		{name: store + ".Store.Get", want: idGet},
		{name: "Getter.Get", want: idIGet},
		{name: "store.Get", want: store + ".Get"}, // a function: the package qualifies it, not a type
		{name: "Write", want: idWrite},
		{name: "List.Push", want: "(*" + tList + ").Push"},
		{name: "store.List[int].Push", want: "(*" + tList + ").Push"},
		{name: "Load", prefer: cache, want: cache + ".Load"}, // the current directory's package wins
		// Outside the repo.
		{name: "Exit", want: "os.Exit"},
		{name: "os.Exit", want: "os.Exit"},
		{name: "Header.Get", want: "(net/http.Header).Get"}, // no repo type is a Header
		{name: "http.Header.Get", want: "(net/http.Header).Get"},
		{name: "Writer.Write", want: "(io.Writer).Write"},
		{name: "yaml.Node.Decode", want: "(*gopkg.in/yaml.v3.Node).Decode"},
		{name: "Decode", want: "(*gopkg.in/yaml.v3.Node).Decode"},
		{name: "Write", want: idWrite}, // the repo's wins over io.Writer's
	} {
		f, err := ix.Func(tc.name, tc.prefer)
		if err != nil || f.ID != tc.want {
			t.Errorf("Func(%q, %q) = %v, %v; want %s", tc.name, tc.prefer, f, err, tc.want)
		}
	}
}

func TestFuncErrors(t *testing.T) {
	ix := testIndex()

	_, err := ix.Func("Get", store) // four in store: prefer doesn't settle it
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("Func(Get) error = %v, want AmbiguousError", err)
	}
	var ids []string
	for _, f := range amb.Candidates {
		ids = append(ids, f.ID)
	}
	want := []string{idGet, "(" + tCache + ").Get", idIGet, store + ".Get"}
	slices.Sort(want)
	if !slices.Equal(ids, want) { // not the local type's Get, nor http.Header's: the repo's win
		t.Errorf("candidates = %v, want %v", ids, want)
	}

	if _, err := ix.Func("Load", ""); !errors.As(err, &amb) {
		t.Errorf("Func(Load) error = %v, want AmbiguousError", err)
	}
	var nf *NotFoundError
	for _, name := range []string{"Nope", "main$1", "Cache.Write", "cache.Store.Get"} {
		if _, err := ix.Func(name, ""); !errors.As(err, &nf) {
			t.Errorf("Func(%q) error = %v, want NotFoundError", name, err)
		}
	}
}

func TestCallers(t *testing.T) {
	ix := testIndex()
	format := func(s Site) string { return fmt.Sprintf("%s %s %s via=%s", s.Kind, s.Pos, s.From, s.Via) }
	for _, tc := range []struct {
		id   string
		want []string
	}{
		{idGet, []string{
			// Direct first, by line as a number: 9 before 17.
			"direct main.go:9 " + idMain + "$1 via=",
			"direct main.go:17 " + idMain + " via=",
			"interface main.go:20 " + idMain + " via=" + tGetter,
			"value main.go:50 " + idMain + " via=" + tStore + ".onGet",
		}},
		// Through an interface from outside the repo.
		{idWrite, []string{"interface main.go:30 " + idMain + " via=io.Writer"}},
		// Passed to a function as a value.
		{idHandle, []string{"value main.go:40 " + idMain + " via=net/http.HandleFunc"}},
		// An interface method: calls through it are direct; no implementations to follow.
		{idIGet, []string{"direct main.go:20 " + idMain + " via="}},
		// Outside the repo.
		{"os.Exit", []string{"direct main.go:60 " + idMain + " via="}},
		{"(io.Writer).Write", []string{"direct main.go:30 " + idMain + " via="}},
	} {
		f, err := ix.Func(tc.id, "")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, s := range ix.Callers(f) {
			got = append(got, format(s))
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("Callers(%s):\ngot  %q\nwant %q", tc.id, got, tc.want)
		}
	}
}

func TestShort(t *testing.T) {
	ix := testIndex()
	for id, want := range map[string]string{
		idGet:                        "(*store.Store).Get",
		idMain + "$1":                "main.main$1",
		tStore + ".onGet":            "store.Store.onGet",
		store:                        "store",
		"(*" + tList + ").Push":      "(*store.List[T]).Push",
		"net/http.HandleFunc":        "http.HandleFunc",
		"(io.Writer).Write":          "(io.Writer).Write",
		"(error).Error":              "(error).Error",
		"gopkg.in/yaml.v3.Node":      "yaml.Node",
		"(*github.com/x/y/v2.T).M":   "(*y.T).M",
		"github.com/x/y/v2/z.Config": "z.Config",
	} {
		if got := ix.Short(id); got != want {
			t.Errorf("Short(%q) = %q, want %q", id, got, want)
		}
	}
}
