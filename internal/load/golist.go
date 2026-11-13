// grepo's loader: `go list` for metadata, then parsing and type-checking
// here, dependencies without function bodies.

package load

import (
	"bufio"
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
	"github.com/samippp/grepo/internal/store"
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
	Module     *listedModule
	Standard   bool // in the Go distribution
	DepOnly    bool // a dependency, not matched by ./...
	Error      *struct{ Pos, Err string }
}

// listedModule is a package's module, as go list reports it.
type listedModule struct {
	Path      string
	Version   string
	Replace   *listedModule // what it's replaced with, if anything
	Main      bool
	GoVersion string
}

const listFields = "ImportPath,Name,Dir,GoFiles,CgoFiles,Imports,ImportMap,Module,Standard,DepOnly,Error"

// loader is one fromGoList run's shared state.
type loader struct {
	fset   *token.FileSet
	sizes  types.Sizes
	env    goEnv
	checks map[string]*check // by import path
	cpu    chan struct{}     // one token per core
	cache  *cache            // nil without a store
	walk   Walk              // nil: repo packages keep their syntax
	repo   map[string]bool   // repo packages' import paths, for walk

	mu      sync.Mutex
	checked []string // type-checked from source
}

// goEnv is what the loader asks the go command for.
type goEnv struct {
	GOOS, GOARCH string // GOARCH sets type sizes: unsafe.Sizeof(x)
	GOROOT       string // for naming stdlib files
	GOVERSION    string
	GOMODCACHE   string
}

// fromGoList loads like GoPackages, without go/packages.
func fromGoList(ctx context.Context, dir string, st *store.Store, log *slog.Logger, walk Walk) (*Result, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	log.Debug("listing packages", "dir", abs)
	list, err := goList(ctx, abs, log)
	if err != nil {
		return nil, err
	}
	ld, err := newLoader(ctx, abs)
	if err != nil {
		return nil, err
	}
	log.Debug("listed packages", "count", len(list), "took", logging.Since(start))
	if st != nil {
		if ld.cache, err = newCache(ctx, st, abs, ld.env); err != nil {
			log.Warn("not using the store", "err", err)
		}
	}

	ld.walk = walk
	for _, lp := range list {
		ld.checks[lp.ImportPath] = &check{lp: lp, done: make(chan struct{})}
		if !lp.DepOnly {
			ld.repo[lp.ImportPath] = true
		}
	}
	// One package per core at a time, each once its imports are done, so only
	// packages in progress hold syntax trees (repo ones too, with a walk).
	var wg sync.WaitGroup
	for _, c := range ld.checks {
		wg.Go(func() { c.run(ld) })
	}
	wg.Wait()

	cached := 0
	if ld.cache != nil {
		cached = len(ld.cache.used)
		if err := ld.cache.save(); err != nil {
			log.Warn("saving to the store", "err", err)
		}
	}

	res := &Result{Dir: abs, Checked: slices.Sorted(slices.Values(ld.checked))}
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
	log.Info("loaded packages", "module", res.Module, "count", len(res.Packages),
		"checked", len(ld.checked), "cached", cached, "took", logging.Since(start))
	return res, nil
}

// goList runs `go list` on dir's packages and their dependencies. -e reports
// broken packages instead of failing; without -compiled, cgo files are
// checked as written, with no C compiler. A first load also downloads
// dependencies, which go list reports on stderr: we log when that starts,
// since a silent wait looks hung.
func goList(ctx context.Context, dir string, log *slog.Logger) ([]*listed, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-e", "-deps", "-json="+listFields, "--", "./...")
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	pipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("go list in %s: %w", dir, err)
	}
	var stderr strings.Builder
	var downloads int
	var downloadStart time.Time
	for sc := bufio.NewScanner(pipe); sc.Scan(); {
		stderr.WriteString(sc.Text() + "\n")
		if mod, ok := strings.CutPrefix(sc.Text(), "go: downloading "); ok {
			if downloads == 0 {
				downloadStart = time.Now()
				log.Info("downloading dependencies (first run on this repo)")
			}
			downloads++
			log.Debug("downloading", "module", mod)
		}
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("go list in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	if downloads > 0 {
		log.Info("downloaded dependencies", "modules", downloads, "took", logging.Since(downloadStart))
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

// newLoader asks the go command for the target's environment (goEnv).
func newLoader(ctx context.Context, dir string) (*loader, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "-json", "GOOS", "GOARCH", "GOROOT", "GOVERSION", "GOMODCACHE")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go env: %w", err)
	}
	var env goEnv
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("go env: %w", err)
	}
	return &loader{
		fset:   token.NewFileSet(),
		sizes:  types.SizesFor("gc", env.GOARCH),
		env:    env,
		checks: map[string]*check{},
		repo:   map[string]bool{},
		cpu:    make(chan struct{}, runtime.GOMAXPROCS(0)),
	}, nil
}

// check type-checks one listed package; done closes when its types are
// ready, before a repo package's walk.
type check struct {
	lp    *listed
	done  chan struct{}
	types *types.Package // nil if it couldn't be checked
	pkg   *Package       // repo packages only
	key   string         // with a store: its cache key, ready with types
	saved bool           // a repo package's row is in the store already, maybe without a share
}

func (c *check) run(ld *loader) {
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

	// With a store: a dependency's types from its export data, a repo
	// package's graph share from its row.
	var srcs []source
	cacheable := false
	if ld.cache != nil {
		identity := fileIdentity(c.lp, ld.env.GOVERSION, ld.env.GOMODCACHE)
		if identity == "" {
			srcs = readSources(c.lp)
		}
		var readable bool
		c.key, readable = ld.cache.key(c.lp, identity, srcs, ld.checks)
		cacheable = readable && c.lp.Error == nil
		if cacheable && c.fromStore(ld, srcs) {
			close(c.done)
			c.walk(ld) // a repo package from the store: the walk takes its share
			return
		}
	}
	if srcs == nil {
		srcs = readSources(c.lp)
	}
	files := ld.parseFiles(c.lp, srcs, repo, report)
	tpkg, info := ld.typeCheck(c.lp, files, repo, report)
	c.types = tpkg
	ld.mu.Lock()
	ld.checked = append(ld.checked, c.lp.ImportPath)
	ld.mu.Unlock()
	saved := false
	if ld.cache != nil {
		// No row for a repo package with errors: it's mid-edit, and its
		// messages hold absolute paths.
		saved = ld.cache.checked(c.key, tpkg, ld.fset, repo, cacheable && len(errs) == 0, c.saved)
	}
	close(c.done) // dependents only need the types, not this package's walk

	if repo {
		c.pkg = c.newPackage(ld, tpkg)
		c.pkg.Errors = errs
		c.pkg.Syntax = files
		c.pkg.TypesInfo = info
		if saved {
			c.pkg.Key = c.key
		}
		c.walk(ld)
	}
}

// fromStore takes c from the store if it can: a dependency's types from its
// export data, a repo package's share. A repo package's types are checked
// from source without bodies: export data holds only the exported API, and
// can differ from source (borrowsMethods).
func (c *check) fromStore(ld *loader, srcs []source) bool {
	if c.lp.DepOnly {
		tpkg := ld.cache.load(c.key, c.lp.ImportPath, ld.fset, c.deps(ld))
		if tpkg == nil {
			return false
		}
		c.types = tpkg
		return true
	}
	var share []byte
	share, c.saved = ld.cache.share(c.key)
	if share == nil {
		return false
	}
	if srcs == nil { // a repo inside the module cache: its files have an identity, so weren't read for the key
		srcs = readSources(c.lp)
	}
	// It had no errors when saved under this key; without bodies it can
	// report unused imports.
	ignore := func(error) {}
	c.types, _ = ld.typeCheck(c.lp, ld.parseFiles(c.lp, srcs, false, ignore), false, ignore)
	c.pkg = c.newPackage(ld, c.types)
	c.pkg.Key = c.key
	c.pkg.Share = share
	return true
}

// deps is the types of every package c imports, directly or not, by path:
// all its export data can reference. All are done, so their types are set.
func (c *check) deps(ld *loader) map[string]*types.Package {
	deps := map[string]*types.Package{}
	seen := map[string]bool{}
	var visit func(path string)
	visit = func(path string) {
		d := ld.checks[path]
		if d == nil || seen[path] {
			return
		}
		seen[path] = true
		if d.types != nil {
			deps[path] = d.types
		}
		for _, imp := range d.lp.Imports {
			visit(imp)
		}
	}
	for _, imp := range c.lp.Imports {
		visit(imp)
	}
	return deps
}

// walk hands a repo package to the load's walk, then drops what only the
// walk needed: its syntax and type information, or its share.
func (c *check) walk(ld *loader) {
	if c.pkg == nil || ld.walk == nil {
		return
	}
	ld.walk(c.pkg, ld.repo)
	c.pkg.Syntax, c.pkg.TypesInfo, c.pkg.Share = nil, nil, nil
}

// newPackage is a repo package's Package, without what only checking gives.
func (c *check) newPackage(ld *loader, tpkg *types.Package) *Package {
	return &Package{
		PkgPath: c.lp.ImportPath,
		Name:    c.lp.Name,
		Dir:     c.lp.Dir,
		GoFiles: goFiles(c.lp),
		Imports: slices.Sorted(slices.Values(c.lp.Imports)),
		Fset:    ld.fset,
		Types:   tpkg,
	}
}

// parseFiles parses a package's files: in full, with comments, for a repo
// package to walk; otherwise declarations only. Stdlib files are named
// $GOROOT/src/..., as export data names them, so positions don't depend on
// where Go is installed.
func (ld *loader) parseFiles(lp *listed, srcs []source, full bool, report func(error)) []*ast.File {
	mode := parser.SkipObjectResolution
	if full {
		mode = parser.AllErrors | parser.ParseComments
	}
	var files []*ast.File
	for _, src := range srcs {
		path := src.path
		name := path
		if lp.Standard {
			if rel, err := filepath.Rel(filepath.Join(ld.env.GOROOT, "src"), path); err == nil {
				name = "$GOROOT/src/" + filepath.ToSlash(rel)
			}
		}
		if src.err != nil {
			report(posError{path + ":1", src.err.Error()})
			continue
		}
		f, err := parser.ParseFile(ld.fset, name, src.data, mode)
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

// typeCheck checks a package's files: in full, with bodies and type
// information, for a repo package to walk; otherwise declarations only,
// and info is nil.
func (ld *loader) typeCheck(lp *listed, files []*ast.File, full bool, report func(error)) (*types.Package, *types.Info) {
	var info *types.Info
	if full {
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
		Importer:         importer{lp, ld.checks},
		Sizes:            ld.sizes,
		IgnoreFuncBodies: !full,
		FakeImportC:      true, // cgo files as written: no C.x errors, no importer call for "C"
		Error: func(err error) {
			if terr, ok := errors.AsType[types.Error](err); ok {
				report(posError{ld.fset.Position(terr.Pos).String(), terr.Msg})
			} else {
				report(err)
			}
		},
	}
	if m := lp.Module; m != nil && m.GoVersion != "" {
		conf.GoVersion = "go" + m.GoVersion
	}
	tpkg := types.NewPackage(lp.ImportPath, lp.Name)
	_ = types.NewChecker(&conf, ld.fset, tpkg, info).Files(files) // errors went to conf.Error
	return tpkg, info
}

// source is one of a package's files, read.
type source struct {
	path string // absolute
	data []byte
	err  error
}

func readSources(lp *listed) []source {
	var srcs []source
	for _, path := range goFiles(lp) {
		data, err := os.ReadFile(path)
		srcs = append(srcs, source{path, data, err})
	}
	return srcs
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
