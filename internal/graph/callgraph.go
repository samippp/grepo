package graph

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/packages"

	"github.com/samippp/grepo/internal/load"
)

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

// CallGraph holds the repo's functions and the calls made inside them.
type CallGraph struct {
	Functions []Function     `json:"functions"`
	Calls     []FunctionCall `json:"calls"`
}

// BuildCallGraph records every function and call in res. Rules (see tests):
//   - IDs from FullName(), types from types.TypeString(t, nil)
//   - closure calls belong to the enclosing func; built-ins/conversions aren't calls
//   - external calls kept; interface calls point at the interface method
//   - dynamic calls have no Callee; one edge per call site; output sorted
func BuildCallGraph(res *load.Result) *CallGraph {
	b := &callGraphBuilder{
		root:      res.Dir,
		functions: []Function{},
		calls:     []FunctionCall{},
	}

	for _, pkg := range res.Packages {
		b.pkg = pkg
		for _, syntaxTree := range pkg.Syntax {
			for _, decl := range syntaxTree.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					b.handleFuncDecl(d)
				case *ast.GenDecl:
					// TODO implement this shit
				}
			}
		}
	}

	// TODO: sort b.functions and b.calls
	return &CallGraph{Functions: b.functions, Calls: b.calls}
}

// callGraphBuilder holds walk context and results.
type callGraphBuilder struct {
	root string            // repo root
	pkg  *packages.Package // current package

	functions []Function
	calls     []FunctionCall
}

func (b *callGraphBuilder) handleFuncDecl(funcDecl *ast.FuncDecl) {
	info := b.pkg.TypesInfo

	// Missing only if the package has type errors; skip it.
	fn, ok := info.Defs[funcDecl.Name].(*types.Func)
	if !ok {
		return
	}

	var recv *Param
	if rs := handleFields(funcDecl.Recv, info); len(rs) > 0 {
		recv = &rs[0]
	}

	b.functions = append(b.functions, Function{
		ID:       fn.FullName(),
		Name:     funcDecl.Name.Name,
		Package:  b.pkg.PkgPath,
		Receiver: recv,
		Params:   handleFields(funcDecl.Type.Params, info),
		Results:  handleFields(funcDecl.Type.Results, info),
		Exported: funcDecl.Name.IsExported(),
		Pos:      b.pos(funcDecl.Pos()),
	})

	// TODO: walk funcDecl.Body for calls
}

// pos formats p as "file:line" relative to the repo root.
func (b *callGraphBuilder) pos(p token.Pos) string {
	position := b.pkg.Fset.Position(p)
	return rel(b.root, position.Filename) + ":" + strconv.Itoa(position.Line)
}

// Parses a field list into a list of Params
func handleFields(fields *ast.FieldList, info *types.Info) []Param {
	if fields == nil {
		return nil
	}
	var params []Param
	for _, f := range fields.List {
		typ := types.TypeString(info.TypeOf(f.Type), nil)

		if len(f.Names) == 0 { // f.Names is []*ast.Ident
			params = append(params, Param{Type: typ})
			continue
		}
		for _, name := range f.Names { // name is *ast.Ident
			params = append(params, Param{Name: name.Name, Type: typ})
		}
	}
	return params
}

// CountByKind counts calls per kind. The StaticCall share is the resolution rate.
func (g *CallGraph) CountByKind() map[CallKind]int {
	counts := make(map[CallKind]int)
	for _, c := range g.Calls {
		counts[c.CallKind]++
	}
	return counts
}
