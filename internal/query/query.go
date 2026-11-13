// Package query answers questions about one repo's graph: which function a
// name means, and every site that can reach it. It reads only the graph
// (graph.PackageGraph and graph.CallGraph), so the commands work the same
// however the graph was built. The CLI formats what it returns.
//
// The indexes here are what a query reads. The store will keep them per
// package later, so a query on an unchanged repo needn't build the whole
// graph (docs/architecture.md, Queries).
package query

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/samippp/grepo/internal/graph"
)

// Index is a repo's graph, indexed for queries.
type Index struct {
	g        *graph.CallGraph
	module   string
	names    map[string]string                 // repo package path → name
	funcs    map[string]*graph.Function        // by ID, outside ones too
	byName   map[string][]*graph.Function      // by Name: no closures, no methods of local types
	outside  map[string][]*graph.Function      // functions outside the repo the repo calls, by Name
	byCallee map[string][]*graph.FunctionCall  // calls with a known callee
	impls    map[string][]graph.Implementation // by concrete type ID
}

// New indexes a repo's graph.
func New(pg *graph.PackageGraph, cg *graph.CallGraph) *Index {
	ix := &Index{
		g:        cg,
		module:   pg.Module,
		names:    map[string]string{},
		funcs:    map[string]*graph.Function{},
		byName:   map[string][]*graph.Function{},
		outside:  map[string][]*graph.Function{},
		byCallee: map[string][]*graph.FunctionCall{},
		impls:    map[string][]graph.Implementation{},
	}
	for _, p := range pg.Packages {
		ix.names[p.Path] = p.Name
	}
	for i := range cg.Functions {
		f := &cg.Functions[i]
		ix.funcs[f.ID] = f
		if f.Parent == "" && !strings.Contains(f.Owner, "#") { // local type IDs hold "#n"
			ix.byName[f.Name] = append(ix.byName[f.Name], f)
		}
	}
	for i := range cg.Calls {
		if c := &cg.Calls[i]; c.Callee != "" {
			ix.byCallee[c.Callee] = append(ix.byCallee[c.Callee], c)
		}
	}
	for _, im := range cg.Implementations {
		ix.impls[im.Type] = append(ix.impls[im.Type], im)
	}
	// Functions outside the repo have no node, but calls to them are kept:
	// their IDs say enough to name them, so callers of os.Exit can be asked.
	for id := range ix.byCallee {
		if ix.funcs[id] != nil {
			continue
		}
		if f := outsideFunc(id); f != nil {
			ix.funcs[id] = f
			ix.outside[f.Name] = append(ix.outside[f.Name], f)
		}
	}
	return ix
}

// outsideFunc is a node for a callee outside the repo, made from its ID:
// os.Exit, (*net/http.Server).Serve, (io.Writer).Write. It has no position.
func outsideFunc(id string) *graph.Function {
	if strings.HasPrefix(id, "(") {
		i := strings.LastIndex(id, ").")
		if i < 0 {
			return nil
		}
		owner := strings.TrimPrefix(id[1:i], "*")
		pkg := ""
		if j := strings.LastIndex(stripTypeArgs(owner), "."); j >= 0 {
			pkg = owner[:j]
		}
		return &graph.Function{ID: id, Name: id[i+2:], Package: pkg, Owner: owner}
	}
	i := strings.LastIndex(id, ".")
	if i < 0 {
		return nil
	}
	return &graph.Function{ID: id, Name: id[i+1:], Package: id[:i]}
}

// IsPackage reports whether id is a repo package's path, as a Site's From is
// for package-level code.
func (ix *Index) IsPackage(id string) bool {
	_, ok := ix.names[id]
	return ok
}

// NotFoundError is a name no function has, in the repo or called from it.
type NotFoundError struct{ Name, Module string }

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no function or method named %s in %s or called from it", e.Name, e.Module)
}

// AmbiguousError is a name several functions have.
type AmbiguousError struct {
	Name       string
	Candidates []*graph.Function // sorted by ID
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("%s is ambiguous: %d functions match", e.Name, len(e.Candidates))
}

// Func returns the function name means: its ID, or its name qualified as
// much as needed to pick it out: Get, Store.Get, store.Store.Get,
// simple/store.Store.Get. Receivers can be written as in Go, (*Store).Get,
// and type args are ignored, List[T].Push. A package qualifier is the
// package's name or a suffix of its path. When several functions match and
// exactly one is in the package prefer (an import path; "" for none), that
// one wins. Closures and methods of local types are found by ID only.
//
// Functions outside the repo that it calls can be named too (os.Exit,
// http.Server.Serve), but one in the repo wins over them: qualify the
// outside one to get it.
func (ix *Index) Func(name, prefer string) (*graph.Function, error) {
	if f := ix.funcs[name]; f != nil {
		return f, nil
	}
	clean := stripTypeArgs(strings.NewReplacer("(", "", ")", "", "*", "").Replace(name))
	qual, fname := "", clean
	if i := strings.LastIndex(clean, "."); i >= 0 {
		qual, fname = clean[:i], clean[i+1:]
	}

	matches := ix.matching(ix.byName[fname], qual)
	if len(matches) == 0 {
		matches = ix.matching(ix.outside[fname], qual)
	}
	switch len(matches) {
	case 0:
		return nil, &NotFoundError{Name: name, Module: ix.module}
	case 1:
		return matches[0], nil
	}
	if prefer != "" {
		var in []*graph.Function
		for _, f := range matches {
			if f.Package == prefer {
				in = append(in, f)
			}
		}
		if len(in) == 1 {
			return in[0], nil
		}
	}
	slices.SortFunc(matches, func(a, b *graph.Function) int { return cmp.Compare(a.ID, b.ID) })
	return nil, &AmbiguousError{Name: name, Candidates: matches}
}

// matching is the functions in fs that qual fits.
func (ix *Index) matching(fs []*graph.Function, qual string) []*graph.Function {
	var out []*graph.Function
	for _, f := range fs {
		if ix.qualifies(f, qual) {
			out = append(out, f)
		}
	}
	return out
}

// qualifies reports whether qual, the part of a name before the function's
// own, fits f: its type for a method, its package, or both.
func (ix *Index) qualifies(f *graph.Function, qual string) bool {
	if qual == "" {
		return true
	}
	if f.Owner == "" {
		return ix.isPackage(qual, f.Package)
	}
	owner := stripTypeArgs(f.Owner)
	typeName := owner[strings.LastIndex(owner, ".")+1:]
	if qual == typeName {
		return true
	}
	i := strings.LastIndex(qual, ".")
	return i >= 0 && qual[i+1:] == typeName && ix.isPackage(qual[:i], f.Package)
}

// isPackage reports whether q names the package at path: the path, the
// package's name, or a suffix of the path (reverseproxy,
// caddyhttp/reverseproxy). Outside the repo, the name is a guess from the
// path (yaml for gopkg.in/yaml.v3).
func (ix *Index) isPackage(q, path string) bool {
	return q == path || q == ix.names[path] || strings.HasSuffix(path, "/"+q) || q == lastElem(path)
}

// stripTypeArgs drops bracketed type args or params: List[T].Push → List.Push.
func stripTypeArgs(s string) string {
	if !strings.Contains(s, "[") {
		return s
	}
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '[':
			depth++
		case r == ']' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Kind is how a site can reach a function.
type Kind string

const (
	Direct    Kind = "direct"    // calls it, by name or through the interface method it is
	Interface Kind = "interface" // calls an interface method its type implements: may run it
	Value     Kind = "value"     // passes or stores it as a func value, which may be called later
)

var kindOrder = map[Kind]int{Direct: 0, Interface: 1, Value: 2}

// Site is one place that can reach a function.
type Site struct {
	Kind Kind
	Pos  string // "file:line", relative to the repo root
	From string // ID of the function the site is in; a package path for package-level code

	// Interface: the interface's ID. Value: the function it's passed to ("" if
	// that's a func value too), or the field or package var it's stored in.
	Via string

	Call   *graph.FunctionCall // the call at the site; nil when a value is stored
	Access *graph.Access       // a value stored: the write
}

// Callers returns every site that can reach fn: direct calls, then calls
// through an interface fn's type implements, then uses of fn as a func value
// (http.HandleFunc("/", h.serve), cobra.Command{RunE: run}); each group by
// position.
//
// Not followed yet: a promoted method reached through an interface the
// embedding type implements but its own type doesn't.
func (ix *Index) Callers(fn *graph.Function) []Site {
	var sites []Site
	for _, c := range ix.byCallee[fn.ID] {
		sites = append(sites, Site{Kind: Direct, Pos: c.Pos, From: c.Caller, Call: c})
	}
	if fn.Owner != "" {
		// A call through an embedded interface names the method where it's
		// declared, (io.Writer).Write, and a type implementing the embedding
		// interface implements that one too.
		for _, im := range ix.impls[fn.Owner] {
			for _, c := range ix.byCallee["("+im.Interface+")."+fn.Name] {
				sites = append(sites, Site{Kind: Interface, Pos: c.Pos, From: c.Caller, Via: im.Interface, Call: c})
			}
		}
	}
	// Uses as a value: only this query needs them, so they're scanned for
	// rather than indexed.
	for i := range ix.g.Calls {
		c := &ix.g.Calls[i]
		if slices.ContainsFunc(c.Args, func(a graph.Arg) bool { return a.Func == fn.ID }) {
			sites = append(sites, Site{Kind: Value, Pos: c.Pos, From: c.Caller, Via: c.Callee, Call: c})
		}
	}
	for i := range ix.g.Accesses {
		if a := &ix.g.Accesses[i]; a.Func == fn.ID {
			sites = append(sites, Site{Kind: Value, Pos: a.Pos, From: a.Accessor, Via: a.Target, Access: a})
		}
	}
	slices.SortStableFunc(sites, func(a, b Site) int {
		return cmp.Or(cmp.Compare(kindOrder[a.Kind], kindOrder[b.Kind]), comparePos(a.Pos, b.Pos), cmp.Compare(a.From, b.From))
	})
	return sites
}

// comparePos orders "file:line" positions by file, then line as a number.
func comparePos(a, b string) int {
	af, al := splitPos(a)
	bf, bl := splitPos(b)
	return cmp.Or(cmp.Compare(af, bf), cmp.Compare(al, bl))
}

// splitPos splits "file:line"; line is 0 if it's missing.
func splitPos(pos string) (file string, line int) {
	i := strings.LastIndex(pos, ":")
	if i < 0 {
		return pos, 0
	}
	line, _ = strconv.Atoi(pos[i+1:])
	return pos[:i], line
}

// Short is id with each package path cut to the package's name, as Go code
// writes it: (*github.com/x/store.Store).Get → (*store.Store).Get. Packages
// outside the repo get their path's last element (net/http.Handler →
// http.Handler), without a major version (yaml.v3, /v2).
func (ix *Index) Short(id string) string {
	var b strings.Builder
	for i := 0; i < len(id); {
		j := i
		for j < len(id) && pathChar(id[j]) {
			j++
		}
		if j == i {
			b.WriteByte(id[i])
			i++
			continue
		}
		b.WriteString(ix.shortToken(id[i:j]))
		i = j
	}
	return b.String()
}

// shortToken shortens one run of path characters: a package path, maybe
// followed by .Name and more (pkg.Store.users).
func (ix *Index) shortToken(tok string) string {
	if name, ok := ix.names[tok]; ok { // a package-level site's From
		return name
	}
	slash := strings.LastIndex(tok, "/")
	// The longest repo package path the token starts with, followed by a dot.
	for k := len(tok) - 1; k > slash; k-- {
		if tok[k] == '.' {
			if name, ok := ix.names[tok[:k]]; ok {
				return name + tok[k:]
			}
		}
	}
	if slash < 0 {
		return tok // fmt.Println, io.Writer, error, or the .Name after a ")"
	}
	dot := strings.IndexByte(tok[slash:], '.')
	if dot < 0 {
		return lastElem(tok)
	}
	path := tok[:slash+dot]
	rest := tok[slash+dot:]
	// gopkg.in/yaml.v3.Node: the version is part of the path.
	if seg, after, ok := strings.Cut(rest[1:], "."); ok && isMajorVersion(seg) {
		path, rest = path+"."+seg, "."+after
	}
	return lastElem(path) + rest
}

// lastElem is the package name a path most likely has: its last element,
// skipping a major version (/v2) and dropping one (yaml.v3).
func lastElem(path string) string {
	elems := strings.Split(path, "/")
	last := elems[len(elems)-1]
	if isMajorVersion(last) && len(elems) > 1 {
		last = elems[len(elems)-2]
	}
	if base, v, ok := strings.Cut(last, "."); ok && isMajorVersion(v) {
		last = base
	}
	return last
}

// isMajorVersion reports whether s is v2, v3, ...
func isMajorVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	n, err := strconv.Atoi(s[1:])
	return err == nil && n >= 2
}

// pathChar reports whether c can be part of a package path or a name after it.
func pathChar(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		c == '/' || c == '.' || c == '-' || c == '_' || c == '~'
}
