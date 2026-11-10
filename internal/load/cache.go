// Cache keys: what a package's cached types depend on (docs/architecture.md,
// Store). Keys are chained: a package's holds its imports' keys, so a change
// anywhere below it gives it a new one.

package load

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/gcexportdata"

	"github.com/samippp/grepo/internal/store"
)

// keyInput is everything a package's key is made from.
type keyInput struct {
	// The load config, the same for every package in a load.
	Grepo     string // hash of grepo's binary
	GOOS      string
	GOARCH    string // type sizes
	GoVersion string // the toolchain's GOVERSION
	ModCache  string // GOMODCACHE: export data names its files by absolute path

	// The package.
	ImportPath      string
	Repo            bool   // a repo package, not a dependency
	ModuleGoVersion string // its go.mod's go line: which language features type-check
	Imports         []string
	ImportMap       map[string]string

	Files      []fileKey // sorted by path
	Identity   string    // module@version or std@GOVERSION; "" if Files carry content hashes
	Root       string    // with hashed Files: the repo root, since export data holds absolute positions
	ImportKeys []string  // in Imports order
}

// fileKey is one file in a key: its path relative to the repo root (its name
// alone when the package has an identity), and its content's sha256 ("" when
// the package has an identity).
type fileKey struct {
	Path string
	Hash string
}

// packageKey hashes k. JSON keeps fields apart and sorts ImportMap.
func packageKey(k keyInput) string {
	data, err := json.Marshal(k)
	if err != nil {
		panic(err) // strings, bools and slices of them always marshal
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// fileIdentity is what stands for a package's file contents when they can't
// change: std@GOVERSION for the stdlib (not for a devel toolchain, whose
// GOROOT may be edited), module@version for a package in the module cache
// (the replacement's, when replaced). "" means hash the files: the repo, a
// local replace, vendor, a workspace module.
func fileIdentity(lp *listed, goVersion, modCache string) string {
	if lp.Standard {
		if strings.HasPrefix(goVersion, "devel") {
			return ""
		}
		return "std@" + goVersion
	}
	m := lp.Module
	if m == nil || modCache == "" || !within(modCache, lp.Dir) {
		return ""
	}
	if m.Replace != nil {
		m = m.Replace
	}
	if m.Version == "" {
		return ""
	}
	return m.Path + "@" + m.Version
}

// within reports whether path is inside dir. filepath.Rel ignores case on
// Windows, as its file system does.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// grepoVersion is a hash of grepo's binary: any change to grepo, or to the
// go/types it's built with, changes every key.
var grepoVersion = sync.OnceValues(func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	f, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
})

// cache is one load's use of the store: it reads packages' export data and
// collects what to write back, saved in one transaction at the end.
type cache struct {
	ctx    context.Context
	st     *store.Store
	root   string
	config keyInput // the load config fields only

	mu   sync.Mutex
	rows []store.Row
	used []string // keys read from the store
}

func newCache(ctx context.Context, st *store.Store, root string, env goEnv) (*cache, error) {
	grepo, err := grepoVersion()
	if err != nil {
		return nil, fmt.Errorf("hashing grepo's binary: %w", err)
	}
	return &cache{
		ctx:  ctx,
		st:   st,
		root: root,
		config: keyInput{
			Grepo: grepo, GOOS: env.GOOS, GOARCH: env.GOARCH, GoVersion: env.GOVERSION, ModCache: env.GOMODCACHE,
		},
	}, nil
}

// key is lp's cache key, once its imports' keys are ready. readable is false
// if one of its files couldn't be read: its key then says so, and it isn't
// cached.
func (ca *cache) key(lp *listed, identity string, srcs []source, checks map[string]*check) (key string, readable bool) {
	k := ca.config
	k.ImportPath = lp.ImportPath
	k.Repo = !lp.DepOnly
	if lp.Module != nil {
		k.ModuleGoVersion = lp.Module.GoVersion
	}
	k.Imports = lp.Imports
	k.ImportMap = lp.ImportMap
	k.Identity = identity

	readable = true
	if identity != "" {
		for _, name := range slices.Concat(lp.GoFiles, lp.CgoFiles) {
			k.Files = append(k.Files, fileKey{Path: name})
		}
	} else {
		// Hashed files are in the repo, or beside it (a local replace,
		// vendor). The graph shows their positions relative to the root, and
		// export data keeps them absolute, so the same code elsewhere can't
		// share the row.
		k.Root = ca.root
		for _, src := range srcs {
			path, err := filepath.Rel(ca.root, src.path)
			if err != nil {
				path = src.path
			}
			hash := "unreadable"
			if src.err == nil {
				sum := sha256.Sum256(src.data)
				hash = hex.EncodeToString(sum[:])
			} else {
				readable = false
			}
			k.Files = append(k.Files, fileKey{Path: filepath.ToSlash(path), Hash: hash})
		}
	}
	slices.SortFunc(k.Files, func(a, b fileKey) int { return strings.Compare(a.Path, b.Path) })

	for _, imp := range lp.Imports {
		var ik string
		if d := checks[imp]; d != nil {
			ik = d.key
		}
		k.ImportKeys = append(k.ImportKeys, ik)
	}
	return packageKey(k), readable
}

// load returns the types cached under key, or nil. deps is the types of
// every package path imports, directly or not, all done (dependency order):
// what its export data can reference, so the read shares their objects.
//
// A read adds the package it reads to deps, and a new package for each one
// it references but doesn't find: there are none, as deps holds them all.
// Each has every object the read can look up, so the read changes no other
// package, and reads run in parallel.
func (ca *cache) load(key, path string, fset *token.FileSet, deps map[string]*types.Package) *types.Package {
	export, ok, err := ca.st.Export(ca.ctx, key)
	if err != nil || !ok {
		return nil
	}
	tpkg, err := gcexportdata.Read(bytes.NewReader(export), fset, deps, path)
	if err != nil {
		return nil
	}
	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.used = append(ca.used, key)
	return tpkg
}

// share returns the graph share saved under key (nil if none), and whether
// key has a row.
func (ca *cache) share(key string) (share []byte, saved bool) {
	share, saved, err := ca.st.Share(ca.ctx, key)
	if err != nil {
		return nil, false
	}
	return share, saved
}

// checked records a package checked from source: a cacheable one is written
// back unless its row is there already (saved: the key fixes what's in it).
// It reports whether the package has a row after the load.
func (ca *cache) checked(key string, tpkg *types.Package, fset *token.FileSet, cacheable, saved bool) bool {
	var export bytes.Buffer
	if cacheable && !saved {
		if err := gcexportdata.Write(&export, fset, tpkg); err != nil {
			cacheable = false
		}
	}
	ca.mu.Lock()
	defer ca.mu.Unlock()
	if cacheable && !saved {
		ca.rows = append(ca.rows, store.Row{Key: key, ImportPath: tpkg.Path(), Export: export.Bytes()})
	}
	return cacheable
}

func (ca *cache) save() error {
	return ca.st.Save(ca.ctx, ca.rows, ca.used)
}

// borrowsMethods reports whether tpkg declares an interface type defined
// from another named interface (type List metav1.ListInterface): checked
// from source, its methods are the other interface's. Export data copies
// the interface, so read back they'd be new methods of tpkg's type, and the
// graph would name interface calls through it differently. Such a package
// isn't cached; a package that only mentions its types is, since reading
// export data keeps the types already in the map.
func borrowsMethods(tpkg *types.Package) bool {
	scope := tpkg.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		iface, ok := named.Underlying().(*types.Interface)
		if !ok {
			continue
		}
		for m := range iface.ExplicitMethods() {
			if recv := m.Signature().Recv(); recv != nil && types.Unalias(recv.Type()) != named {
				return true
			}
		}
	}
	return false
}
