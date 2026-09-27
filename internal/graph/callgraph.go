package graph

import "github.com/samippp/grepo/internal/load"

type Function struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Package  string  `json:"package"`
	Receiver *Param  `json:"receiver,omitempty"`
	Params   []Param `json:"params"`
	Results  []Param `json:"results"`
	Exported bool    `json:"exported"`
	Pos      string  `json:"pos"`
}

type Param struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type FunctionCall struct {
	Caller         string   `json:"caller"`
	Callee         string   `json:"callee"`
	CallerPosition string   `json:"callerPosition"`
	CallKind       CallKind `json:"callKind"`
}

type CallKind string

const (
	StaticCall    CallKind = "static"
	InterfaceCall CallKind = "interface"
	DynamicCall   CallKind = "dynamic"
)

// CallGraph is every function and method declared in the repo, plus every
// call made from inside them.
type CallGraph struct {
	Functions []Function     `json:"functions"`
	Calls     []FunctionCall `json:"calls"`
}

// BuildCallGraph walks the syntax trees in res and records each function
// declaration and each call inside it. callgraph_test.go defines the expected
// output for the fixture in testdata/simple.
//
// Rules the tests encode (change the tests if you decide differently):
//   - IDs come from types.Func.FullName(), e.g. "(*example.com/simple/store.Store).Get".
//   - Types are written with types.TypeString(t, nil), e.g. "example.com/simple/model.User".
//   - Positions are "file:line", relative to the repo root (see rel in packages.go).
//   - Calls inside a closure belong to the enclosing function. Calling the
//     closure itself is not an edge.
//   - Built-ins (len, append) and type conversions (int64(n)) are not calls.
//   - Calls to code outside the repo (fmt.Println) are kept, since side-effect
//     detection needs them.
//   - Interface calls point at the interface method; dynamic calls have an
//     empty Callee.
//   - One FunctionCall per call site, so repeated calls give repeated edges.
//   - Output is sorted, so repeated runs produce identical JSON.
func BuildCallGraph(res *load.Result) *CallGraph {
	// TODO: your walker goes here.
	return &CallGraph{Functions: []Function{}, Calls: []FunctionCall{}}
}

// CountByKind returns how many calls there are of each kind. The share of
// StaticCall is the resolution rate: how many calls have a known target.
func (g *CallGraph) CountByKind() map[CallKind]int {
	counts := make(map[CallKind]int)
	for _, c := range g.Calls {
		counts[c.CallKind]++
	}
	return counts
}
