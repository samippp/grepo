package graph

import (
	"fmt"
	"testing"
)

func TestCallGraphVariables(t *testing.T) {
	g := buildFixtureCallGraph(t)

	v := func(name, typ, pos string) Variable {
		return Variable{ID: storePkg + "." + name, Name: name, Package: storePkg, Type: typ, Pos: "store/vars.go:" + pos}
	}
	c := func(name, typ, value, pos string, exported bool) Variable {
		x := v(name, typ, pos)
		x.Const, x.Value, x.Exported = true, value, exported
		return x
	}
	timeout := v("Timeout", "int", "6")
	timeout.Exported = true

	want := []Variable{
		v("defaultStore", "*"+tStore, "4"),
		timeout,
		// var a, b = 1, 2: one node per name.
		v("a", "int", "8"),
		v("b", "int", "8"),
		// var first, err = load(): one node per name, one call.
		v("first", "int", "11"),
		v("err", "error", "11"),
		// No nodes for the two "_" vars on lines 16-17.
		c("maxUsers", "untyped int", "100", "21", false),
		c("greeting", "string", `"hi"`, "23", false),
		// iota: Green and Blue have no value in the source, but the type checker knows it.
		c("Red", "untyped int", "0", "26", true),
		c("Green", "untyped int", "1", "27", true),
		c("Blue", "untyped int", "2", "28", true),
		// No node for save's local var count.
		{ID: storePkg + ".Counter", Name: "Counter", Package: storePkg, Type: "int", Exported: true, Pos: wat("25")},
		{ID: storePkg + ".defaultBox", Name: "defaultBox", Package: storePkg, Type: tWBox, Pos: wat("28")},
		{ID: storePkg + ".wrapped", Name: "wrapped", Package: storePkg, Type: storePkg + ".Wrapped", Pos: wat("91")},
		{ID: storePkg + ".limit", Name: "limit", Package: storePkg, Type: "int", Pos: rat("46")},
		{ID: storePkg + ".doubled", Name: "doubled", Package: storePkg, Type: "untyped int", Const: true, Value: "200", Pos: rat("48")},
		{ID: storePkg + ".shared", Name: "shared", Package: storePkg, Type: tRHolder, Pos: rat("50")},
		{ID: storePkg + ".OnReady", Name: "OnReady", Package: storePkg, Type: "func()", Exported: true, Pos: rat("52")},
		{ID: storePkg + ".table", Name: "table", Package: storePkg, Type: "[100]int", Pos: rat("164")},
		{ID: storePkg + ".hooks", Name: "hooks", Package: storePkg, Type: "func()", Pos: cat("49")},
		{ID: storePkg + ".fallback", Name: "fallback", Package: storePkg, Type: "func()", Pos: cat("49")},
		{ID: storePkg + ".nested", Name: "nested", Package: storePkg, Type: "func()", Pos: cat("53")},
	}

	checkExact(t, "variable", g.Variables, want, func(v Variable) string { return fmt.Sprintf("%+v", v) })
}
