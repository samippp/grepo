package main

import (
	"fmt"
	"unsafe"

	"example.com/dep"
)

// Sizes come from the target's GOARCH: the loader must match go/packages.
const (
	ptrSize  = unsafe.Sizeof(uintptr(0))
	boxAlign = unsafe.Alignof(dep.Box[int64]{})
	buf      = dep.Size * 2
)

type shout struct{ dep.Plain }

func main() {
	b := dep.Box[string]{V: "hi"}
	var g dep.Greeter = shout{dep.Plain{Prefix: ">"}}
	fmt.Println(b.Get(), g.Greet("you"), ptrSize, boxAlign, buf)
}
