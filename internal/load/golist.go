// grepo's own loader: `go list` for metadata, then parsing and type-checking
// here, dependencies without function bodies. Replaces go/packages in Load.

package load

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samippp/grepo/internal/logging"
)

// listed is one package from `go list -json`, with the fields we use.
type listed struct {
	ImportPath string
	Name       string
	Dir        string
	GoFiles    []string
	CgoFiles   []string
	Imports    []string          // resolved: vendored paths differ from the source
	ImportMap  map[string]string // source path → resolved, when they differ
	Module     *struct {
		Path      string
		Main      bool
		GoVersion string
	}
	Standard bool // in the Go distribution
	DepOnly  bool // a dependency, not matched by ./...
	Error    *struct{ Pos, Err string }
}

const listFields = "ImportPath,Name,Dir,GoFiles,CgoFiles,Imports,ImportMap,Module,Standard,DepOnly,Error"

// loader is one fromGoList run's shared state.
type loader struct {
	fset   *token.FileSet
	sizes  types.Sizes
	goroot string            // the target's, for naming stdlib files
	checks map[string]*check // by import path
	cpu    chan struct{}     // one token per core
}

// fromGoList loads like GoPackages, without go/packages.
func fromGoList(ctx context.Context, dir string, log *slog.Logger) (*Result, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	// The first load of a repo also downloads its dependencies, which is
	// usually most of the time spent here.
	start := time.Now()
	log.Debug("listing packages", "dir", abs)
	list, err := goList(ctx, abs)
	if err != nil {
		return nil, err
	}
	ld, err := newLoader(ctx, abs)
	if err != nil {
		return nil, err
	}
	log.Debug("listed packages", "count", len(list), "took", logging.Since(start))

	for _, lp := range list {
		ld.checks[lp.ImportPath] = &check{lp: lp, done: make(chan struct{})}
	}
	// Parsing and type-checking are CPU-bound: one package per core at a time.
	// A package starts once its imports are done, so independent ones run in
	// parallel and only the packages in progress hold syntax trees.
	var wg sync.WaitGroup
	for _, c := range ld.checks {
		wg.Go(func() { c.run(ld) })
	}
	wg.Wait()

	res := &Result{Dir: abs}
	for _, lp := range list { // go list order: dependencies first
		c := ld.checks[lp.ImportPath]
		if lp.DepOnly {
			continue
		}
		if res.Module == "" && lp.Module != nil && lp.Module.Main {
			res.Module = lp.Module.Path
		}
		for _, e := range c.pkg.Errors {
			log.Warn("package has errors", "package", lp.ImportPath, "err", e.Error())
		}
		res.Packages = append(res.Packages, c.pkg)
	}
	if len(res.Packages) == 0 {
		return nil, fmt.Errorf("no Go packages found in %s", abs)
	}
	log.Info("loaded packages", "module", res.Module, "count", len(res.Packages), "took", logging.Since(start))
	return res, nil
}

// goList runs `go list` on every package under dir and its dependencies.
// -e reports broken packages instead of failing; no -compiled, so cgo files
// are type-checked as written and no C compiler is needed.
func goList(ctx context.Context, dir string) ([]*listed, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-e", "-deps", "-json="+listFields, "--", "./...")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	var list []*listed
	for dec := json.NewDecoder(&stdout); ; {
		lp := new(listed)
		if err := dec.Decode(lp); err == io.EOF {
			return list, nil
		} else if err != nil {
			return nil, fmt.Errorf("reading go list output: %w", err)
		}
		list = append(list, lp)
	}
}

// newLoader asks the go command for the target's GOARCH (type sizes, which
// consts like unsafe.Sizeof(x) depend on) and GOROOT.
func newLoader(ctx context.Context, dir string) (*loader, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "GOARCH", "GOROOT")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go env: %w", err)
	}
	env := strings.Fields(string(out))
	if len(env) != 2 {
		return nil, fmt.Errorf("go env: unexpected output %q", out)
	}
	return &loader{
		fset:   token.NewFileSet(),
		sizes:  types.SizesFor("gc", env[0]),
		goroot: env[1],
		checks: map[string]*check{},
		cpu:    make(chan struct{}, runtime.GOMAXPROCS(0)),
	}, nil
}

// check type-checks one listed package; done closes when its types are ready.
type check struct {
	lp    *listed
	done  chan struct{}
	types *types.Package // nil if it couldn't be checked
	pkg   *Package       // repo packages only
}

func (c *check) run(ld *loader) {
	defer close(c.done)
	repo := !c.lp.DepOnly
	var errs []error
	report := func(err error) {
		if repo { // dependencies: dropped bodies leave unused imports, and they're not ours to report
			errs = append(errs, err)
		}
	}
	if c.lp.Error != nil {
		report(posError{c.lp.Error.Pos, strings.TrimSpace(c.lp.Error.Err)})
	}

	for _, imp := range c.lp.Imports {
		if d := ld.checks[imp]; d != nil {
			<-d.done
		}
	}
	ld.cpu <- struct{}{}
	defer func() { <-ld.cpu }()

	files := ld.parseFiles(c.lp, repo, report)

	var info *types.Info
	if repo {
		info = &types.Info{
			Types:        map[ast.Expr]types.TypeAndValue{},
			Defs:         map[*ast.Ident]types.Object{},
			Uses:         map[*ast.Ident]types.Object{},
			Implicits:    map[ast.Node]types.Object{},
			Instances:    map[*ast.Ident]types.Instance{},
			Scopes:       map[ast.Node]*types.Scope{},
			Selections:   map[*ast.SelectorExpr]*types.Selection{},
			FileVersions: map[*ast.File]string{},
		}
	}
	conf := types.Config{
		Importer:         importer{c.lp, ld.checks},
		Sizes:            ld.sizes,
		IgnoreFuncBodies: !repo,
		FakeImportC:      true, // cgo files as written: no C.x errors, no importer call for "C"
		Error: func(err error) {
			if terr, ok := errors.AsType[types.Error](err); ok {
				report(posError{ld.fset.Position(terr.Pos).String(), terr.Msg})
			} else {
				report(err)
			}
		},
	}
	if m := c.lp.Module; m != nil && m.GoVersion != "" {
		conf.GoVersion = "go" + m.GoVersion
	}

	tpkg := types.NewPackage(c.lp.ImportPath, c.lp.Name)
	_ = types.NewChecker(&conf, ld.fset, tpkg, info).Files(files) // errors went to conf.Error
	c.types = tpkg

	if repo {
		c.pkg = &Package{
			PkgPath:   c.lp.ImportPath,
			Name:      c.lp.Name,
			Dir:       c.lp.Dir,
			GoFiles:   goFiles(c.lp),
			Imports:   slices.Sorted(slices.Values(c.lp.Imports)),
			Errors:    errs,
			Fset:      ld.fset,
			Syntax:    files,
			Types:     tpkg,
			TypesInfo: info,
		}
	}
}

// parseFiles parses a package's files: repo packages whole, with comments,
// as the walk reads them; dependencies without comments or object resolution.
// Stdlib files are named $GOROOT/src/..., as compiler export data names them,
// so positions in the Go distribution don't depend on where it's installed.
func (ld *loader) parseFiles(lp *listed, repo bool, report func(error)) []*ast.File {
	mode := parser.SkipObjectResolution
	if repo {
		mode = parser.AllErrors | parser.ParseComments
	}
	var files []*ast.File
	for _, path := range goFiles(lp) {
		name := path
		if lp.Standard {
			if rel, err := filepath.Rel(filepath.Join(ld.goroot, "src"), path); err == nil {
				name = "$GOROOT/src/" + filepath.ToSlash(rel)
			}
		}
		src, err := os.ReadFile(path)
		if err != nil {
			report(posError{path + ":1", err.Error()})
			continue
		}
		f, err := parser.ParseFile(ld.fset, name, src, mode)
		if list, ok := errors.AsType[scanner.ErrorList](err); ok {
			for _, e := range list {
				report(posError{e.Pos.String(), e.Msg})
			}
		} else if err != nil {
			report(posError{name + ":1", err.Error()})
		}
		if f != nil {
			files = append(files, f)
		}
	}
	return files
}

// goFiles is the package's Go files, cgo ones included, as absolute paths.
func goFiles(lp *listed) []string {
	var files []string
	for _, name := range slices.Concat(lp.GoFiles, lp.CgoFiles) {
		files = append(files, filepath.Join(lp.Dir, name))
	}
	return files
}

// importer hands a package the types of its imports, already checked.
type importer struct {
	lp     *listed
	checks map[string]*check
}

func (im importer) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if resolved, ok := im.lp.ImportMap[path]; ok {
		path = resolved
	}
	if c := im.checks[path]; c != nil && c.types != nil {
		return c.types, nil
	}
	return nil, fmt.Errorf("no types for %s", path)
}

// posError is a list, parse or type error, formatted like go/packages'.
type posError struct{ pos, msg string }

func (e posError) Error() string {
	pos := e.pos
	if pos == "" {
		pos = "-"
	}
	return pos + ": " + e.msg
}
