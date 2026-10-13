// Package vtadiff compares grepo's call graph with the one x/tools builds with
// VTA, and reports where they differ. It's a dev tool for finding bugs in the
// walker, not part of the analysis pipeline.
//
// VTA describes the program differently (closures are separate functions,
// package-level calls belong to a synthetic init, interface calls point at
// concrete targets), so its edges are translated into grepo's IDs first.
package vtadiff

import (
	"cmp"
	"fmt"
	"go/token"
	"go/types"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/samippp/grepo/internal/graph"
	"github.com/samippp/grepo/internal/load"
)

// Category is one kind of difference, with an example line per occurrence.
type Category struct {
	Name    string
	Meaning string
	Items   []string
}

// Report is the result of a comparison.
type Report struct {
	Module   string
	Ours     int // grepo call edges
	Theirs   int // VTA edges after translation, excluding expected ones
	Matched  int // identical caller, callee and line
	Expected map[string]int
	Cats     []*Category
}

// edge is a VTA edge translated into grepo's IDs.
type edge struct {
	caller, callee, pos string
	targetType          string // receiver type of the callee, for interface checks
}

// Compare builds VTA's call graph from the same loaded packages as cg and
// reports the differences.
func Compare(res *load.Result, cg *graph.CallGraph) *Report {
	tr := newTranslator(res, cg)
	theirs, expected := tr.vtaEdges()

	r := &Report{Module: res.Module, Ours: len(cg.Calls), Theirs: len(theirs), Expected: expected}
	onlyOurs := r.cat("Only in grepo", "a call grepo records at a site where VTA has no edge: likely a grepo bug, or a call VTA can't see")
	differs := r.cat("Different callee", "same call site, but grepo and VTA name a different function: very likely a bug in one of them")
	onlyVTA := r.cat("Only in VTA", "an edge at a site where grepo recorded no call: likely a call grepo misses")
	nearLine := r.cat("Line differs", "same caller and callee a few lines apart: usually a call expression split across lines")
	dynResolved := r.cat("Dynamic calls VTA resolved", "grepo marked the call dynamic; VTA found these targets")
	dynNone := r.cat("Dynamic calls neither resolves", "a call through a function value that VTA can't resolve either (e.g. a callback parameter)")
	ifaceOK := r.cat("Interface targets confirmed", "VTA target's type implements the interface, per grepo's Implementations")
	ifaceNotImpl := r.cat("Interface targets not in Implementations", "repo interface, repo target type, but grepo records no implementation: likely a grepo bug")
	ifaceOutside := r.cat("Interface targets outside the repo", "the interface or the target type has no grepo node, so Implementations can't confirm it (by design)")
	ifaceNone := r.cat("Interface calls with no VTA target", "VTA found no concrete type reaching the call")

	// Positions are file:line, so a site can hold several calls. Match within
	// each site by callee: static by exact ID, interface by method name, and
	// whatever is left over goes to dynamic calls on the same line.
	theirsAt := map[string][]edge{}
	for _, e := range theirs {
		k := site(e.caller, e.pos)
		theirsAt[k] = append(theirsAt[k], e)
	}
	oursAt := map[string][]graph.FunctionCall{}
	var sites []string
	for _, c := range cg.Calls {
		k := site(c.Caller, c.CallerPosition)
		if oursAt[k] == nil {
			sites = append(sites, k)
		}
		oursAt[k] = append(oursAt[k], c)
	}

	used := map[edge]bool{}
	var unmatchedOurs []graph.FunctionCall
	for _, k := range sites {
		calls, targets := oursAt[k], theirsAt[k]

		var leftoverStatic []graph.FunctionCall
		matched := map[string]bool{} // callees matched on this line
		for _, c := range calls {
			if c.CallKind != graph.StaticCall {
				continue
			}
			// The same call twice on one line looks identical (no column),
			// and VTA's copies were merged, so a repeat counts as matched.
			if es := take(targets, used, func(e edge) bool { return e.callee == c.Callee }); len(es) > 0 || matched[c.Callee] {
				matched[c.Callee] = true
				r.Matched++
			} else {
				leftoverStatic = append(leftoverStatic, c)
			}
		}

		for _, c := range calls {
			if c.CallKind != graph.InterfaceCall {
				continue
			}
			es := take(targets, used, func(e edge) bool { return methodName(e.callee) == methodName(c.Callee) })
			if len(es) == 0 {
				ifaceNone.add("%s → %s @ %s", c.Caller, c.Callee, c.CallerPosition)
			}
			iface := tr.owner[c.Callee] // "" if the interface isn't a repo type
			for _, e := range es {
				line := fmt.Sprintf("%s → %s @ %s: target %s (%s)", c.Caller, c.Callee, c.CallerPosition, e.callee, e.targetType)
				switch {
				case iface == "" || !tr.repoTypes[e.targetType]:
					ifaceOutside.add("%s", line)
				case tr.implements[e.targetType+"|"+iface]:
					ifaceOK.add("%s", line)
				default:
					ifaceNotImpl.add("%s", line)
				}
			}
		}

		for _, c := range calls {
			if c.CallKind != graph.DynamicCall {
				continue
			}
			es := take(targets, used, func(edge) bool { return true })
			if len(es) == 0 {
				dynNone.add("%s @ %s", c.Caller, c.CallerPosition)
			}
			for _, e := range es {
				dynResolved.add("%s @ %s → %s", c.Caller, c.CallerPosition, e.callee)
			}
		}

		// A static call left over next to a leftover VTA edge on the same line
		// means the two disagree about the callee.
		for _, c := range leftoverStatic {
			if es := take(targets, used, func(edge) bool { return true }); len(es) > 0 {
				differs.add("%s @ %s: grepo → %s, VTA → %s", c.Caller, c.CallerPosition, c.Callee, calleeList(es))
			} else {
				unmatchedOurs = append(unmatchedOurs, c)
			}
		}
	}

	var unmatchedTheirs []edge
	for _, e := range theirs {
		if !used[e] {
			unmatchedTheirs = append(unmatchedTheirs, e)
		}
	}

	// Pair leftovers with the same caller and callee a few lines apart.
	for _, c := range unmatchedOurs {
		if i := nearby(unmatchedTheirs, c); i >= 0 {
			nearLine.add("%s → %s: grepo @ %s, VTA @ %s", c.Caller, c.Callee, c.CallerPosition, unmatchedTheirs[i].pos)
			unmatchedTheirs = slices.Delete(unmatchedTheirs, i, i+1)
			continue
		}
		onlyOurs.add("%s → %s @ %s", c.Caller, c.Callee, c.CallerPosition)
	}
	for _, e := range unmatchedTheirs {
		onlyVTA.add("%s → %s @ %s", e.caller, e.callee, e.pos)
	}

	for _, c := range r.Cats {
		slices.Sort(c.Items)
	}
	return r
}

// Write prints the report, with at most limit examples per category (0 = all).
func (r *Report) Write(w io.Writer, limit int) error {
	var b strings.Builder
	fmt.Fprintf(&b, "VTA comparison for %s\n", r.Module)
	fmt.Fprintf(&b, "  grepo calls: %d   VTA edges: %d   matched exactly: %d\n", r.Ours, r.Theirs, r.Matched)
	for _, c := range r.Cats {
		fmt.Fprintf(&b, "\n== %s (%d) ==\n   %s\n", c.Name, len(c.Items), c.Meaning)
		for i, item := range c.Items {
			if limit > 0 && i == limit {
				fmt.Fprintf(&b, "   ... and %d more\n", len(c.Items)-limit)
				break
			}
			fmt.Fprintf(&b, "   %s\n", item)
		}
	}
	fmt.Fprintf(&b, "\n== Expected differences (not compared) ==\n")
	reasons := make([]string, 0, len(r.Expected))
	for reason := range r.Expected {
		reasons = append(reasons, reason)
	}
	slices.Sort(reasons)
	for _, reason := range reasons {
		fmt.Fprintf(&b, "   %-45s %d\n", reason, r.Expected[reason])
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func (r *Report) cat(name, meaning string) *Category {
	c := &Category{Name: name, Meaning: meaning}
	r.Cats = append(r.Cats, c)
	return c
}

func (c *Category) add(format string, args ...any) {
	c.Items = append(c.Items, fmt.Sprintf(format, args...))
}

// translator maps VTA's SSA functions onto grepo's IDs.
type translator struct {
	res        *load.Result
	cg         *graph.CallGraph // for its ID functions
	fset       *token.FileSet
	roots      map[string]bool   // repo package paths
	initByPos  map[string]string // "file:line" → grepo ID, for init functions
	owner      map[string]string // grepo function ID → owner type ID
	repoTypes  map[string]bool
	implements map[string]bool // "type|interface"
}

func newTranslator(res *load.Result, cg *graph.CallGraph) *translator {
	t := &translator{
		res:        res,
		cg:         cg,
		roots:      map[string]bool{},
		initByPos:  map[string]string{},
		owner:      map[string]string{},
		repoTypes:  map[string]bool{},
		implements: map[string]bool{},
	}
	for _, p := range res.Packages {
		t.roots[p.PkgPath] = true
		t.fset = p.Fset
	}
	for _, f := range cg.Functions {
		t.owner[f.ID] = f.Owner
		if f.Name == "init" && f.Receiver == nil && f.Owner == "" {
			t.initByPos[f.Pos] = f.ID
		}
	}
	for _, ty := range cg.Types {
		t.repoTypes[ty.ID] = true
	}
	for _, im := range cg.Implementations {
		t.implements[im.Type+"|"+im.Interface] = true
	}
	return t
}

// vtaEdges builds VTA's call graph and returns its edges from repo code,
// translated into grepo's IDs, plus counts of edges skipped as expected.
func (t *translator) vtaEdges() ([]edge, map[string]int) {
	prog, _ := ssautil.Packages(t.res.Packages, ssa.InstantiateGenerics)
	prog.Build()
	cg := vta.CallGraph(ssautil.AllFunctions(prog), nil)

	seen := map[edge]bool{}
	var edges []edge
	expected := map[string]int{}
	callgraph.GraphVisitEdges(cg, func(e *callgraph.Edge) error {
		if e.Site == nil || !e.Site.Pos().IsValid() {
			return nil
		}
		caller, ok := t.callerID(e.Caller.Func)
		if !ok {
			return nil // not repo code, or a synthetic wrapper
		}
		callee, reason := t.calleeID(e.Callee.Func)
		if reason != "" {
			expected[reason]++
			return nil
		}
		ed := edge{caller: caller, callee: callee, pos: t.pos(e.Site.Pos()), targetType: t.recvTypeID(e.Callee.Func)}
		if !seen[ed] { // generic instances repeat the same edge
			seen[ed] = true
			edges = append(edges, ed)
		}
		return nil
	})
	slices.SortFunc(edges, func(a, b edge) int {
		return cmp.Or(cmp.Compare(a.caller, b.caller), cmp.Compare(a.pos, b.pos), cmp.Compare(a.callee, b.callee))
	})
	return edges, expected
}

// callerID maps a calling function to grepo's caller: closures belong to the
// enclosing function, and the package initializer is the package itself.
func (t *translator) callerID(fn *ssa.Function) (string, bool) {
	for fn.Parent() != nil {
		fn = fn.Parent()
	}
	if o := fn.Origin(); o != nil {
		fn = o
	}
	if fn.Pkg == nil || !t.roots[fn.Pkg.Pkg.Path()] {
		return "", false
	}
	if fn.Synthetic == "package initializer" {
		return fn.Pkg.Pkg.Path(), true
	}
	if fn.Synthetic != "" {
		return "", false
	}
	return t.funcID(fn)
}

// calleeID maps a called function to grepo's callee ID, or gives the reason
// the edge is an expected difference.
func (t *translator) calleeID(fn *ssa.Function) (id, reason string) {
	if fn.Parent() != nil {
		return "", "callee is a closure (grepo has no closure nodes)"
	}
	if o := fn.Origin(); o != nil {
		fn = o
	}
	if fn.Synthetic == "package initializer" {
		return "", "package initializer calls another package's init"
	}
	obj, ok := fn.Object().(*types.Func)
	if !ok {
		return "", "synthetic callee: " + fn.Synthetic
	}
	if obj.Name() == "init" && obj.Signature().Recv() == nil {
		return "", "package initializer calls an init function"
	}
	return t.cg.FuncID(obj), ""
}

// funcID is grepo's ID for a declared function: the graph's FuncID, except init
// functions, which grepo numbers per file and are found by position.
func (t *translator) funcID(fn *ssa.Function) (string, bool) {
	obj, ok := fn.Object().(*types.Func)
	if !ok {
		return "", false
	}
	if obj.Name() == "init" && obj.Signature().Recv() == nil {
		id, ok := t.initByPos[t.pos(fn.Pos())]
		return id, ok
	}
	return t.cg.FuncID(obj), true
}

// pos formats p as "file:line" relative to the repo root, like grepo does.
func (t *translator) pos(p token.Pos) string {
	position := t.fset.Position(p)
	r, err := filepath.Rel(t.res.Dir, position.Filename)
	if err != nil {
		r = position.Filename
	}
	return filepath.ToSlash(r) + ":" + strconv.Itoa(position.Line)
}

// recvTypeID is the type ID of fn's receiver (pointer and aliases removed).
func (t *translator) recvTypeID(fn *ssa.Function) string {
	var recv *types.Var
	if fn.Signature.Recv() != nil {
		recv = fn.Signature.Recv()
	} else if obj, ok := fn.Object().(*types.Func); ok {
		recv = obj.Signature().Recv()
	}
	if recv == nil {
		return ""
	}
	typ := recv.Type()
	if p, ok := typ.(*types.Pointer); ok {
		typ = p.Elem()
	}
	named, ok := types.Unalias(typ).(*types.Named)
	if !ok {
		return ""
	}
	return t.cg.TypeID(named.Origin().Obj())
}

func site(caller, pos string) string { return caller + "@" + pos }

// take marks and returns the not-yet-used edges that match.
func take(edges []edge, used map[edge]bool, match func(edge) bool) []edge {
	var out []edge
	for _, e := range edges {
		if !used[e] && match(e) {
			used[e] = true
			out = append(out, e)
		}
	}
	return out
}

func calleeList(edges []edge) string {
	var names []string
	for _, e := range edges {
		names = append(names, e.callee)
	}
	slices.Sort(names)
	return strings.Join(slices.Compact(names), ", ")
}

// methodName is the part after the last dot: "(pkg.Getter).Get" → "Get".
func methodName(id string) string {
	return id[strings.LastIndex(id, ".")+1:]
}

// nearby finds an unmatched VTA edge with c's caller and callee within 3 lines.
func nearby(edges []edge, c graph.FunctionCall) int {
	file, line := splitPos(c.CallerPosition)
	for i, e := range edges {
		if e.caller != c.Caller || e.callee != c.Callee {
			continue
		}
		f, l := splitPos(e.pos)
		if f == file && l >= line-3 && l <= line+3 {
			return i
		}
	}
	return -1
}

func splitPos(pos string) (string, int) {
	i := strings.LastIndex(pos, ":")
	line, _ := strconv.Atoi(pos[i+1:])
	return pos[:i], line
}
