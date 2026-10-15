package store

import (
	"net/http"
	"os"

	"example.com/simple/model"
)

// Writes: see TestCallGraphAccesses for the edge each line produces.

type Inner struct{ X int }

type Box struct {
	N     int
	Name  string
	Items []int
	Inner Inner   // struct value: writes through it change Box's memory
	Ptr   *Inner  // pointer: writes through it don't
	Buf   [4]byte // array: element writes change Box's memory
	Users map[int]model.User
	Tags  []model.Tagged
}

var Counter int

// Package-level initializers: construct by the package.
var defaultBox = Box{Name: "d"}

func (b *Box) Writes(o *Box, src []byte) {
	b.N = 1
	b.N++
	b.N += 2
	b.N = b.N + 1
	b.N = o.N + 1
	b.Items = append(b.Items, 1)
	b.Users[1] = model.User{}
	b.Users[2] = model.User{Name: "a"}
	b.Items[0]++
	delete(b.Users, 1)
	clear(b.Items)
	copy(b.Buf[:], src)
	b.Buf[0] = 1
	b.Inner.X = 1
	b.Ptr.X = 1
	*b.Ptr = Inner{}
	b.Tags[0].Name = "t"
	Counter = 3
	b.N, Counter = 4, 5
}

func locals() {
	local := Box{}
	local.N = 1
	local = Box{N: 2}
	p := &local
	*p = Box{}
}

func build() *Box {
	return &Box{N: 1, Inner: Inner{X: 2}}
}

func points() []Inner {
	return []Inner{
		{X: 1},
		{2},
	}
}

func outside() *http.Server {
	os.Args = nil
	http.DefaultClient.Timeout = 0
	return &http.Server{Addr: ":80"}
}

func (b *Box) ranges(xs []int) {
	for b.N = range xs {
	}
}

// Explicit deref: the same write as b.Ptr.X = 1.
func (b *Box) derefs() {
	(*b.Ptr).X = 1
}

// An embedded pointer: writing wrapped.X changes what Wrapped.Inner points to,
// not wrapped itself.
type Wrapped struct{ *Inner }

var wrapped Wrapped

func promoted() {
	wrapped.X = 1
}
