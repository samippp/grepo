package load

import (
	"fmt"
	"go/types"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/samippp/grepo/internal/store"
)

// copyFixture copies testdata/mod to a temp dir, so tests can edit it.
func copyFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir("testdata/mod", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("testdata/mod", path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func mustLoad(t *testing.T, dir string, st *store.Store) *Result {
	t.Helper()
	res, err := Load(t.Context(), dir, st, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// repoPaths is res's packages' import paths, sorted.
func repoPaths(res *Result) []string {
	var paths []string
	for _, p := range res.Packages {
		paths = append(paths, p.PkgPath)
	}
	slices.Sort(paths)
	return paths
}

// uncached is testdata/mod's dependency that's never cached: it defines
// interfaces from other named interfaces (borrowsMethods).
const uncached = "example.com/dep/named"

// checkChecked fails unless res type-checked exactly its repo packages,
// uncached and extra.
func checkChecked(t *testing.T, res *Result, extra ...string) {
	t.Helper()
	want := slices.Sorted(slices.Values(append(append(repoPaths(res), uncached), extra...)))
	if !slices.Equal(res.Checked, want) {
		t.Errorf("checked %v\nwant %v", res.Checked, want)
	}
}

func edit(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s doesn't contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A load through the store gives what a load without one gives. The next
// load takes every dependency from it: only repo packages are checked.
func TestCachedLoad(t *testing.T) {
	dir := copyFixture(t)
	st := openStore(t)

	cold := mustLoad(t, dir, st)
	diff(t, describe(cold), describe(mustLoad(t, dir, nil)))
	if len(cold.Checked) <= len(cold.Packages) {
		t.Errorf("the first load checked %v, want its dependencies too", cold.Checked)
	}

	warm := mustLoad(t, dir, st)
	diff(t, describe(warm), describe(cold))
	checkChecked(t, warm)

	// describe leaves out positions outside the repo; the graph reads them
	// (a field of type T tls.ConnectionState), and export data keeps lines.
	positions := func(res *Result) map[string]string {
		p := res.Packages[slices.IndexFunc(res.Packages, func(p *Package) bool { return p.PkgPath == "example.com/loadfix" })]
		at := map[string]string{}
		for _, imp := range p.Types.Imports() {
			for _, name := range []string{"Println", "Box"} {
				if obj := imp.Scope().Lookup(name); obj != nil {
					pos := p.Fset.Position(obj.Pos())
					at[imp.Path()+"."+name] = fmt.Sprintf("%s:%d", pos.Filename, pos.Line)
				}
			}
		}
		return at
	}
	want := map[string]string{
		"fmt.Println":         "$GOROOT/src/fmt/print.go:" + strings.Split(positions(cold)["fmt.Println"], ":")[1],
		"example.com/dep.Box": filepath.Join(dir, "dep/dep.go") + ":8",
	}
	for _, res := range []*Result{cold, warm} {
		if got := positions(res); !maps.Equal(got, want) {
			t.Errorf("outside positions %v, want %v", got, want)
		}
	}
}

// After an edit, a load through the store gives what a load without one
// gives, and checks again only what the edit changed (repo packages are
// always checked).
func TestCachedLoadAfterEdits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edit    func(t *testing.T, dir string)
		recheck []string // dependencies the edit changes, besides uncached
	}{
		{"dep body", func(t *testing.T, dir string) {
			edit(t, filepath.Join(dir, "dep/dep.go"), `p.Prefix + " " + name`, `p.Prefix + ": " + name`)
		}, []string{"example.com/dep", "example.com/dep/wrap"}},
		{"dep signature", func(t *testing.T, dir string) {
			edit(t, filepath.Join(dir, "dep/dep.go"), "func (b Box[T]) Get() T { return b.V }",
				"func (b Box[T]) Get() T { return b.V }\n\nfunc (b Box[T]) Set(v T) Box[T] { b.V = v; return b }")
		}, []string{"example.com/dep", "example.com/dep/wrap"}},
		{"dep go version", func(t *testing.T, dir string) {
			edit(t, filepath.Join(dir, "dep/go.mod"), "go 1.21", "go 1.20") // lower: a higher one needs the main module's raised
		}, []string{"example.com/dep", "example.com/dep/wrap"}},
		{"moved replace", func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "third_party"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(dir, "dep"), filepath.Join(dir, "third_party/dep")); err != nil {
				t.Fatal(err)
			}
			edit(t, filepath.Join(dir, "go.mod"), "=> ./dep", "=> ./third_party/dep")
		}, []string{"example.com/dep", "example.com/dep/wrap"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyFixture(t)
			st := openStore(t)
			mustLoad(t, dir, st)
			tc.edit(t, dir)

			got := mustLoad(t, dir, st)
			diff(t, describe(got), describe(mustLoad(t, dir, nil)))
			checkChecked(t, got, tc.recheck...)
		})
	}
}

// The same code in another directory reuses the stdlib's and the module
// cache's rows, but not those of packages whose files are hashed (the repo,
// a local replace): their export data holds absolute positions, which would
// point into the first copy.
func TestCacheKeyedOnLocation(t *testing.T) {
	st := openStore(t)
	mustLoad(t, copyFixture(t), st)
	dir := copyFixture(t)
	res := mustLoad(t, dir, st)
	checkChecked(t, res, "example.com/dep", "example.com/dep/wrap")

	p := res.Packages[slices.IndexFunc(res.Packages, func(p *Package) bool { return p.PkgPath == "example.com/loadfix" })]
	i := slices.IndexFunc(p.Types.Imports(), func(imp *types.Package) bool { return imp.Path() == "example.com/dep" })
	greeter := p.Types.Imports()[i].Scope().Lookup("Greeter")
	if file := p.Fset.Position(greeter.Pos()).Filename; !within(dir, file) {
		t.Errorf("dep.Greeter is in %s, outside the copy loaded (%s)", file, dir)
	}
}

// How modules are downloaded isn't in the key: go.sum fixes what they are.
func TestCacheIgnoresGOPROXY(t *testing.T) {
	dir := copyFixture(t)
	st := openStore(t)
	mustLoad(t, dir, st)
	t.Setenv("GOPROXY", "off")
	checkChecked(t, mustLoad(t, dir, st))
}

// Another GOARCH changes type sizes and file selection: nothing is reused.
func TestCacheKeyedOnGOARCH(t *testing.T) {
	dir := copyFixture(t)
	st := openStore(t)
	mustLoad(t, dir, st)
	t.Setenv("GOARCH", "arm64")
	res := mustLoad(t, dir, st)
	for _, path := range []string{"fmt", "example.com/dep"} {
		if !slices.Contains(res.Checked, path) {
			t.Errorf("%s came from the store after GOARCH changed", path)
		}
	}
}

func baseKey() keyInput {
	return keyInput{
		Grepo: "grepo", GOOS: "linux", GOARCH: "amd64", GoVersion: "go1.26.3", ModCache: "/m",
		ImportPath: "example.com/p", ModuleGoVersion: "1.21",
		Imports:    []string{"example.com/q", "fmt"},
		ImportMap:  map[string]string{"q": "example.com/q"},
		Files:      []fileKey{{Path: "p/a.go", Hash: "h1"}},
		ImportKeys: []string{"kq", "kfmt"},
	}
}

// Every input changes the key.
func TestPackageKey(t *testing.T) {
	base := packageKey(baseKey())
	if base == "" || packageKey(baseKey()) != base {
		t.Fatalf("packageKey = %q, want a stable non-empty key", base)
	}
	for name, change := range map[string]func(*keyInput){
		"grepo":            func(k *keyInput) { k.Grepo = "grepo2" },
		"GOOS":             func(k *keyInput) { k.GOOS = "windows" },
		"GOARCH":           func(k *keyInput) { k.GOARCH = "arm64" },
		"toolchain":        func(k *keyInput) { k.GoVersion = "go1.26.4" },
		"module cache":     func(k *keyInput) { k.ModCache = "/other" },
		"import path":      func(k *keyInput) { k.ImportPath = "example.com/r" },
		"role":             func(k *keyInput) { k.Repo = true },
		"module go":        func(k *keyInput) { k.ModuleGoVersion = "1.22" },
		"imports":          func(k *keyInput) { k.Imports = []string{"example.com/q"} },
		"import map":       func(k *keyInput) { k.ImportMap = map[string]string{"q": "vendor/example.com/q"} },
		"file path":        func(k *keyInput) { k.Files[0].Path = "moved/a.go" },
		"file content":     func(k *keyInput) { k.Files[0].Hash = "h2" },
		"added file":       func(k *keyInput) { k.Files = append(k.Files, fileKey{Path: "p/b.go", Hash: "h3"}) },
		"identity":         func(k *keyInput) { k.Identity = "example.com/p@v1.0.0" },
		"root":             func(k *keyInput) { k.Root = "/elsewhere" },
		"an import's key":  func(k *keyInput) { k.ImportKeys[1] = "kfmt2" },
		"field boundaries": func(k *keyInput) { k.GOOS, k.GOARCH = "linuxamd", "64" },
	} {
		k := baseKey()
		change(&k)
		if packageKey(k) == base {
			t.Errorf("changing the %s keeps the key", name)
		}
	}
}

// Contents are hashed unless they can't change: the stdlib of a released
// toolchain, and a module in the module cache.
func TestFileIdentity(t *testing.T) {
	mod := func(path, version string) *listedModule { return &listedModule{Path: path, Version: version} }
	replaced := func(m, by *listedModule) *listedModule { m.Replace = by; return m }
	for _, tc := range []struct {
		name      string
		lp        listed
		goVersion string
		want      string
	}{
		{"stdlib", listed{Standard: true, Dir: "/goroot/src/fmt"}, "go1.26.3", "std@go1.26.3"},
		{"devel stdlib", listed{Standard: true, Dir: "/goroot/src/fmt"}, "devel go1.27-abc123", ""},
		{"module cache", listed{Dir: "/m/example.com/x@v1.2.0/sub", Module: mod("example.com/x", "v1.2.0")}, "go1.26.3",
			"example.com/x@v1.2.0"},
		{"replaced by a version", listed{Dir: "/m/example.com/fork@v1.3.0",
			Module: replaced(mod("example.com/x", "v1.2.0"), mod("example.com/fork", "v1.3.0"))}, "go1.26.3",
			"example.com/fork@v1.3.0"},
		{"local replace", listed{Dir: "/repo/dep", Module: replaced(mod("example.com/dep", "v0.0.0"), mod("./dep", ""))},
			"go1.26.3", ""},
		{"vendored", listed{Dir: "/repo/vendor/example.com/x", Module: mod("example.com/x", "v1.2.0")}, "go1.26.3", ""},
		{"beside the module cache", listed{Dir: "/m-other/example.com/x@v1.2.0", Module: mod("example.com/x", "v1.2.0")},
			"go1.26.3", ""},
		{"repo package", listed{Dir: "/repo/pkg", Module: &listedModule{Path: "example.com/repo", Main: true}}, "go1.26.3", ""},
	} {
		if got := fileIdentity(&tc.lp, tc.goVersion, "/m"); got != tc.want {
			t.Errorf("%s: fileIdentity = %q, want %q", tc.name, got, tc.want)
		}
	}
}
