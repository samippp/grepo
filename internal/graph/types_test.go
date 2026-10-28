package graph

import (
	"fmt"
	"testing"
)

func TestCallGraphTypes(t *testing.T) {
	g := buildFixtureCallGraph(t)

	st, mt := "store/store.go:", "model/kinds.go:"

	want := []Type{
		{ID: tStore, Name: "Store", Package: storePkg, Kind: StructType, Exported: true, Pos: st + "16", Fields: []Field{
			fld(tStore, "mu", "sync.Mutex", st+"17"),
			fld(tStore, "users", "map[int]"+tUser, st+"18"),
		}},
		{ID: tGetter, Name: "Getter", Package: storePkg, Kind: InterfaceType, Exported: true, Pos: st + "12"},
		{ID: tList, Name: "List", Package: mainPkg, Kind: StructType, Exported: true, Pos: "main.go:54",
			Fields: []Field{fld(tList, "items", "[]T", "main.go:54")}},
		{ID: tUser, Name: "User", Package: modelPkg, Kind: StructType, Exported: true, Pos: "model/model.go:4", Fields: []Field{
			fld(tUser, "ID", "int", "model/model.go:5"),
			fld(tUser, "Name", "string", "model/model.go:6"),
		}},
		// Embedded struct field: named after its type, Embedded set.
		{ID: tTagged, Name: "Tagged", Package: modelPkg, Kind: StructType, Exported: true, Pos: mt + "18", Fields: []Field{
			emb(tTagged, "User", tUser, mt+"19"),
			fld(tTagged, "Tag", "string", mt+"20"),
		}},
		// Alias: Target is what it names; no fields or methods of its own.
		{ID: tID, Name: "ID", Package: modelPkg, Kind: AliasType, Exported: true, Pos: mt + "6", Target: "int"},
		// Neither struct nor interface: Underlying says what it is.
		{ID: tUserID, Name: "UserID", Package: modelPkg, Kind: OtherType, Exported: true, Pos: mt + "8", Underlying: "int"},
		// Embedded interface: recorded in Embeds, its methods are not copied.
		{ID: tReadNamer, Name: "ReadNamer", Package: modelPkg, Kind: InterfaceType, Exported: true, Pos: mt + "13", Embeds: []string{"io.Reader"}},
		{ID: tCache, Name: "Cache", Package: storePkg, Kind: StructType, Exported: true, Pos: "store/cache.go:7"},
		{ID: tNamer, Name: "Namer", Package: modelPkg, Kind: InterfaceType, Exported: true, Pos: mt + "24"},
		{ID: tLabel, Name: "Label", Package: modelPkg, Kind: StructType, Exported: true, Pos: mt + "26"},
		{ID: tBadge, Name: "Badge", Package: modelPkg, Kind: StructType, Exported: true, Pos: mt + "30",
			Fields: []Field{emb(tBadge, "Label", tLabel, mt+"30")}},
		{ID: tAny, Name: "Any", Package: modelPkg, Kind: InterfaceType, Exported: true, Pos: mt + "33"},
		{ID: tSource, Name: "Source", Package: mainPkg, Kind: InterfaceType, Exported: true, Pos: "main.go:71"},
		{ID: tS, Name: "S", Package: storePkg, Kind: AliasType, Exported: true, Pos: "store/alias.go:4", Target: tStore},
		// Package-level result keeps its plain ID; the local ones can't collide with it.
		{ID: tResult, Name: "result", Package: mainPkg, Kind: StructType, Pos: "main.go:81",
			Fields: []Field{fld(tResult, "ok", "bool", "main.go:81")}},
		{ID: tParseResult, Name: "result", Package: mainPkg, Kind: StructType, Pos: "main.go:84",
			Fields: []Field{fld(tParseResult, "n", "int", "main.go:84")}},
		// Same name twice in one function: numbered in source order.
		{ID: tSaveResult1, Name: "result", Package: mainPkg, Kind: StructType, Pos: "main.go:89",
			Fields: []Field{fld(tSaveResult1, "err", "error", "main.go:89")}},
		{ID: tSaveResult2, Name: "result", Package: mainPkg, Kind: StructType, Pos: "main.go:91",
			Fields: []Field{fld(tSaveResult2, "msg", "string", "main.go:91")}},
		{ID: tLocalNamer, Name: "namer", Package: mainPkg, Kind: InterfaceType, Pos: "main.go:99"},
		// Inside a closure: the closure is its parent.
		{ID: tJob, Name: "job", Package: mainPkg, Kind: StructType, Pos: "main.go:108",
			Fields: []Field{emb(tJob, "Store", "*"+tStore, "main.go:108")}},
		// Uses wrap's T, but has no type params of its own: no [T] in the ID.
		{ID: tBox, Name: "box", Package: mainPkg, Kind: StructType, Pos: "main.go:114",
			Fields: []Field{fld(tBox, "v", "T", "main.go:114")}},

		{ID: tWInner, Name: "Inner", Package: storePkg, Kind: StructType, Exported: true, Pos: wat("12"),
			Fields: []Field{fld(tWInner, "X", "int", wat("12"))}},
		{ID: tWBox, Name: "Box", Package: storePkg, Kind: StructType, Exported: true, Pos: wat("14"), Fields: []Field{
			fld(tWBox, "N", "int", wat("15")),
			fld(tWBox, "Name", "string", wat("16")),
			fld(tWBox, "Items", "[]int", wat("17")),
			fld(tWBox, "Inner", tWInner, wat("18")),
			fld(tWBox, "Ptr", "*"+tWInner, wat("19")),
			fld(tWBox, "Buf", "[4]byte", wat("20")),
			fld(tWBox, "Users", "map[int]"+tUser, wat("21")),
			fld(tWBox, "Tags", "[]"+tTagged, wat("22")),
		}},
		{ID: storePkg + ".Wrapped", Name: "Wrapped", Package: storePkg, Kind: StructType, Exported: true, Pos: wat("89"),
			Fields: []Field{emb(storePkg+".Wrapped", "Inner", "*"+tWInner, wat("89"))}},

		{ID: tRPart, Name: "Part", Package: storePkg, Kind: StructType, Exported: true, Pos: rat("11"),
			Fields: []Field{fld(tRPart, "X", "int", rat("11"))}},
		{ID: tRBase, Name: "Base", Package: storePkg, Kind: StructType, Exported: true, Pos: rat("17"),
			Fields: []Field{fld(tRBase, "ID", "int", rat("17"))}},
		{ID: tRSub, Name: "Sub", Package: storePkg, Kind: StructType, Exported: true, Pos: rat("19"),
			Fields: []Field{fld(tRSub, "Ptr", "*"+tRPart, rat("19"))}},
		{ID: tRHolder, Name: "Holder", Package: storePkg, Kind: StructType, Exported: true, Pos: rat("21"), Fields: []Field{
			emb(tRHolder, "Base", tRBase, rat("22")),
			fld(tRHolder, "N", "int", rat("23")),
			fld(tRHolder, "Step", "int", rat("24")),
			fld(tRHolder, "Key", "int", rat("25")),
			fld(tRHolder, "Items", "[]int", rat("26")),
			fld(tRHolder, "Users", "map[int]string", rat("27")),
			fld(tRHolder, "Val", tRPart, rat("28")),
			fld(tRHolder, "Ptr", "*"+tRPart, rat("29")),
			fld(tRHolder, "Sub", tRSub, rat("30")),
			fld(tRHolder, "H", "net/http.Handler", rat("31")),
			fld(tRHolder, "OnErr", "func(error)", rat("32")),
			fld(tRHolder, "mu", "sync.Mutex", rat("33")),
		}},
		{ID: tRLocked, Name: "Locked", Package: storePkg, Kind: StructType, Exported: true, Pos: rat("39"),
			Fields: []Field{emb(tRLocked, "Mutex", "sync.Mutex", rat("39"))}},
		{ID: tRBag, Name: "Bag", Package: storePkg, Kind: StructType, Exported: true, Pos: rat("41"),
			Fields: []Field{fld(tRBag, "items", "[]T", rat("41"))}},
		{ID: storePkg + ".Slots", Name: "Slots", Package: storePkg, Kind: OtherType, Exported: true, Pos: rat("44"), Underlying: "[100]byte"},
		{ID: tROuter, Name: "Outer", Package: storePkg, Kind: StructType, Exported: true, Pos: rat("142"), Fields: []Field{
			fld(tROuter, "In", tRHolder, rat("143")),
			fld(tROuter, "L", tRLocked, rat("144")),
		}},

		{ID: tCRunner, Name: "Runner", Package: storePkg, Kind: StructType, Exported: true, Pos: cat("10"), Fields: []Field{
			fld(tCRunner, "OnDone", "func()", cat("11")),
			fld(tCRunner, "Run", "func() error", cat("12")),
		}},
	}

	checkExact(t, "type", g.Types, want, func(ty Type) string { return fmt.Sprintf("%+v", ty) })
}

func TestCallGraphImplementations(t *testing.T) {
	g := buildFixtureCallGraph(t)

	want := []Implementation{
		// Value receiver: Cache and *Cache both implement Getter.
		{Type: tCache, Interface: tGetter},
		// Pointer receiver: only *Store implements Getter.
		{Type: tStore, Interface: tGetter, PointerOnly: true},
		{Type: tLabel, Interface: tNamer},
		// Badge has Name only through its embedded Label (a promoted method).
		{Type: tBadge, Interface: tNamer},
		// Local interfaces take part like package-level ones.
		{Type: tLabel, Interface: tLocalNamer},
		{Type: tBadge, Interface: tLocalNamer},
		// A local struct implements through its embedded *Store, so its value does too.
		{Type: tJob, Interface: tGetter},
		// Nothing for Any: empty interfaces are skipped.
		// Not listed: interfaces themselves, generic List[T] (no type args to check),
		// and interfaces outside the repo (UserID implements fmt.Stringer).
	}

	checkExact(t, "implementation", g.Implementations, want, func(im Implementation) string { return fmt.Sprintf("%+v", im) })
}

// fld is a field of the type owner; emb is an embedded one.
func fld(owner, name, typ, pos string) Field {
	return Field{ID: owner + "." + name, Name: name, Type: typ, Pos: pos}
}

func emb(owner, name, typ, pos string) Field {
	f := fld(owner, name, typ, pos)
	f.Embedded = true
	return f
}
