package load

import (
	"fmt"
	"go/token"
	"go/types"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// graph's fixture has stdlib dependencies only. testdata/mod adds a
// dependency outside the module, a build-tagged file, a test file, sizes from
// GOARCH, a go version and type errors.
var fixtures = []string{"../graph/testdata/simple", "testdata/mod"}

// fromGoList matches go/packages: the same packages, files, imports and
// errors, and every identifier, selection and expression typed the same.
func TestFromGoListMatchesGoPackages(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	for _, dir := range fixtures {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			want, _, err := GoPackages(t.Context(), dir, log)
			if err != nil {
				t.Fatal(err)
			}
			got, err := fromGoList(t.Context(), dir, nil, log)
			if err != nil {
				t.Fatal(err)
			}
			if got.Dir != want.Dir || got.Module != want.Module {
				t.Errorf("dir, module = %s, %s; want %s, %s", got.Dir, got.Module, want.Dir, want.Module)
			}
			// go/packages compiles for export data and adds the compiler's
			// output as one more error, repeating the type errors. We don't compile.
			// Its export data also copies an interface defined from another
			// named one (dep/named), so calls through it use copied methods;
			// checked from source, they're the other interface's.
			copied := func(l string) bool { return strings.HasPrefix(l, "example.com/loadfix: use defined.go:") }
			wantLines := slices.DeleteFunc(describe(want), func(l string) bool {
				return strings.Contains(l, ": error -: # ") || copied(l)
			})
			diff(t, slices.DeleteFunc(describe(got), copied), wantLines)
		})
	}
}

// Stdlib positions read $GOROOT/src/..., as in compiler export data, whatever
// the install path: a type defined from a stdlib struct shows its fields'.
func TestStdlibPositions(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	viaPackages, _, err := GoPackages(t.Context(), "testdata/mod", log)
	if err != nil {
		t.Fatal(err)
	}
	own, err := fromGoList(t.Context(), "testdata/mod", nil, log)
	if err != nil {
		t.Fatal(err)
	}
	for name, res := range map[string]*Result{"go/packages": viaPackages, "fromGoList": own} {
		p := res.Packages[slices.IndexFunc(res.Packages, func(p *Package) bool { return p.PkgPath == "example.com/loadfix" })]
		i := slices.IndexFunc(p.Types.Imports(), func(imp *types.Package) bool { return imp.Path() == "fmt" })
		println := p.Types.Imports()[i].Scope().Lookup("Println")
		if file := p.Fset.Position(println.Pos()).Filename; file != "$GOROOT/src/fmt/print.go" {
			t.Errorf("%s: fmt.Println in %s, want $GOROOT/src/fmt/print.go", name, file)
		}
	}
}

// describe renders a Result one fact per line, positions relative to its
// root. Objects declared outside the repo print without a position: export
// data and source needn't agree on columns there.
func describe(res *Result) []string {
	repo := map[string]bool{}
	for _, p := range res.Packages {
		repo[p.PkgPath] = true
	}
	var lines []string
	for _, p := range res.Packages {
		pos := func(at token.Pos) string {
			position := p.Fset.Position(at)
			rel, _ := filepath.Rel(res.Dir, position.Filename)
			return fmt.Sprintf("%s:%d:%d", filepath.ToSlash(rel), position.Line, position.Column)
		}
		obj := func(o types.Object) string {
			if o == nil {
				return "<nil>"
			}
			s := types.ObjectString(o, nil)
			if o.Pkg() != nil && repo[o.Pkg().Path()] {
				s += " @" + pos(o.Pos())
			}
			return s
		}
		add := func(format string, args ...any) {
			lines = append(lines, p.PkgPath+": "+fmt.Sprintf(format, args...))
		}

		add("name %s dir %s", p.Name, p.Dir)
		for _, f := range p.GoFiles {
			add("file %s", f)
		}
		for _, imp := range p.Imports {
			add("import %s", imp)
		}
		for _, e := range p.Errors {
			add("error %s", e)
		}
		if p.Types == nil || p.TypesInfo == nil {
			add("no types")
			continue
		}
		add("types %s %s", p.Types.Path(), p.Types.Name())
		for _, name := range p.Types.Scope().Names() {
			add("scope %s", obj(p.Types.Scope().Lookup(name)))
		}
		for _, f := range p.Syntax {
			add("syntax %s version %q", pos(f.Pos()), p.TypesInfo.FileVersions[f])
		}

		info := p.TypesInfo
		for id, o := range info.Defs {
			add("def %s %s: %s", pos(id.Pos()), id.Name, obj(o))
		}
		for id, o := range info.Uses {
			add("use %s %s: %s", pos(id.Pos()), id.Name, obj(o))
		}
		for n, o := range info.Implicits {
			add("implicit %s %T: %s", pos(n.Pos()), n, obj(o))
		}
		for e, sel := range info.Selections {
			add("selection %s: %s", pos(e.Pos()), sel)
		}
		for id, inst := range info.Instances {
			add("instance %s %s: %s %s", pos(id.Pos()), id.Name, typeList(inst.TypeArgs), inst.Type)
		}
		for e, tv := range info.Types {
			add("type %s-%s %T: %s", pos(e.Pos()), pos(e.End()), e, typeAndValue(tv))
		}
		for n, s := range info.Scopes {
			add("scope-node %s %T: %v", pos(n.Pos()), n, s.Names())
		}
	}
	return lines
}

func typeList(l *types.TypeList) string {
	var s []string
	for t := range l.Types() {
		s = append(s, t.String())
	}
	return "[" + strings.Join(s, ", ") + "]"
}

func typeAndValue(tv types.TypeAndValue) string {
	var modes []string
	for name, on := range map[string]bool{
		"void": tv.IsVoid(), "type": tv.IsType(), "builtin": tv.IsBuiltin(), "value": tv.IsValue(),
		"nil": tv.IsNil(), "addressable": tv.Addressable(), "assignable": tv.Assignable(), "hasOk": tv.HasOk(),
	} {
		if on {
			modes = append(modes, name)
		}
	}
	slices.Sort(modes)
	s := fmt.Sprintf("%v %s", modes, tv.Type)
	if tv.Value != nil {
		s += " = " + tv.Value.ExactString()
	}
	return s
}

// diff fails for each line missing from got and each line got has extra,
// counting duplicates.
func diff(t *testing.T, got, want []string) {
	t.Helper()
	n := map[string]int{}
	for _, l := range got {
		n[l]++
	}
	for _, l := range want {
		n[l]--
	}
	shown := 0
	for _, l := range slices.Sorted(maps.Keys(n)) {
		for ; n[l] < 0 && shown < 50; n[l]++ {
			t.Errorf("missing %s", l)
			shown++
		}
		for ; n[l] > 0 && shown < 50; n[l]-- {
			t.Errorf("extra %s", l)
			shown++
		}
	}
	if shown == 50 {
		t.Error("more differences not shown")
	}
}
