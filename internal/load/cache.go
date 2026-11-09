// Cache keys: what a package's cached types depend on (docs/architecture.md,
// Store). Keys are chained: a package's holds its imports' keys, so a change
// anywhere below it gives it a new one.

package load

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
	ImportKeys []string  // in Imports order
}

// fileKey is one file in a key: its path relative to the repo root (its name
// alone when the package has an identity), and its content's sha256 ("" when
// the package has an identity).
type fileKey struct {
	Path string
	Hash string
}

// packageKey hashes k.
func packageKey(k keyInput) string {
	return ""
}

// fileIdentity is what stands for a package's file contents when they can't
// change: std@GOVERSION for the stdlib (not for a devel toolchain, whose
// GOROOT may be edited), module@version for a package in the module cache
// (the replacement's, when replaced). "" means hash the files: the repo, a
// local replace, vendor, a workspace module.
func fileIdentity(lp *listed, goVersion, modCache string) string {
	return ""
}
