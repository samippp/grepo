// Cache keys (docs/architecture.md, Store). A package's key holds its
// imports' keys, so a change anywhere below it gives it a new one.

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

// fileKey is one file in a key. With an identity, Path is the file's name and
// Hash is empty; otherwise Path is relative to the root and Hash its sha256.
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

// fileIdentity stands for a package's files when they can't change:
// std@GOVERSION (not on a devel toolchain, whose GOROOT may be edited), or
// module@version in the module cache. "" means hash the files: the repo, a
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

// cache is one load's use of the store: it reads export data and shares, and
// saves new rows in one transaction at the end.
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
		// Export data keeps absolute positions, so hashed files' rows are tied
		// to this root.
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

// load returns the types cached under key, or nil. deps must hold every
// package path imports, directly or not, so the read shares their objects
// and changes no other package: reads run in parallel.
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
// key has a row. A share returned counts as a use of the row.
func (ca *cache) share(key string) (share []byte, saved bool) {
	share, saved, err := ca.st.Share(ca.ctx, key)
	if err != nil {
		return nil, false
	}
	if share != nil {
		ca.mu.Lock()
		ca.used = append(ca.used, key)
		ca.mu.Unlock()
	}
	return share, saved
}

// checked queues a row for a package checked from source, if it's cacheable
// and not saved already. A dependency's row holds its export data; a repo
// package's gets its share once the graph is built. It reports whether the
// package has a row.
func (ca *cache) checked(key string, tpkg *types.Package, fset *token.FileSet, repo, cacheable, saved bool) bool {
	if !cacheable || saved {
		return cacheable
	}
	var export []byte
	if !repo {
		if borrowsMethods(tpkg) {
			return false
		}
		var buf bytes.Buffer
		if err := gcexportdata.Write(&buf, fset, tpkg); err != nil {
			return false
		}
		export = buf.Bytes()
	}
	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.rows = append(ca.rows, store.Row{Key: key, ImportPath: tpkg.Path(), Export: export})
	return true
}

func (ca *cache) save() error {
	return ca.st.Save(ca.ctx, ca.rows, ca.used)
}

// borrowsMethods reports whether tpkg declares an interface defined from
// another named one (type List metav1.ListInterface). From source its
// methods are the other's; export data copies them as tpkg's own, which
// would rename interface calls through it, so its export data isn't cached.
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
