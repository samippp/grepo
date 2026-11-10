package graph

import (
	"strconv"
	"testing"
)

// Calls and accesses that tie on the sort key (one line, one caller, one
// callee or target) keep the order they were walked in, so the graph doesn't
// depend on the order packages were merged in.
func TestSortKeepsTiesInWalkOrder(t *testing.T) {
	g := &CallGraph{}
	for i := range 200 {
		n := strconv.Itoa(i)
		// Other entries around the ties, in reverse, so the sort moves things.
		g.Calls = append(g.Calls, FunctionCall{Caller: "p.f", Pos: "f.go:" + strconv.Itoa(1000-i), Callee: "p.g"})
		g.Calls = append(g.Calls, FunctionCall{Caller: "p.f", Pos: "f.go:1", Callee: "p.g", Args: []Arg{{Expr: n}}})
		g.Accesses = append(g.Accesses, Access{Accessor: "p.f", Pos: "f.go:" + strconv.Itoa(1000-i), Target: "p.v", Kind: ReadAccess})
		g.Accesses = append(g.Accesses, Access{Accessor: "p.f", Pos: "f.go:1", Target: "p.v", Kind: ReadAccess, Func: n})
	}
	g.sort()

	next := 0
	for _, c := range g.Calls {
		if c.Pos != "f.go:1" {
			continue
		}
		if got := c.Args[0].Expr; got != strconv.Itoa(next) {
			t.Fatalf("tied call %d sorted to position %d", mustAtoi(t, got), next)
		}
		next++
	}
	next = 0
	for _, a := range g.Accesses {
		if a.Pos != "f.go:1" {
			continue
		}
		if a.Func != strconv.Itoa(next) {
			t.Fatalf("tied access %d sorted to position %d", mustAtoi(t, a.Func), next)
		}
		next++
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
