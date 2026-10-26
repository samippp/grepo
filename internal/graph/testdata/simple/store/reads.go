package store

import (
	"net/http"
	"os"
	"sync"
)

// Reads and escapes: see TestCallGraphReads for the edges each line produces.

type Part struct{ X int }

func (p *Part) Bump() { p.X++ }

func (p Part) Value() int { return p.X }

type Base struct{ ID int }

type Sub struct{ Ptr *Part }

type Holder struct {
	Base
	N     int
	Step  int
	Key   int
	Items []int
	Users map[int]string
	Val   Part
	Ptr   *Part
	Sub   Sub
	H     http.Handler
	OnErr func(error)
	mu    sync.Mutex
}

func (r *Holder) Touch() {}

// Locked gets Lock from the embedded Mutex.
type Locked struct{ sync.Mutex }

type Bag[T any] struct{ items []T }

// Package-level reads: the package is the accessor.
type Slots [maxUsers]byte

var limit = maxUsers * 2

const doubled = maxUsers * 2

var shared Holder

var OnReady = func() {}

func use(n int) {}

func two(a, b int) {}

func sum(xs []int) int { return 0 }

func keep(p *Part) {}

func (r *Holder) values(xs []int) int {
	y := r.N
	use(r.N)
	if r.N > 0 {
	}
	for range r.Items {
	}
	y += r.Items[r.Key]
	y += r.Val.X
	y += r.Ptr.X
	y += r.ID
	y += len(r.Items)
	copy(xs, r.Items)
	two(r.N, r.N)
	func() { y += r.Step }()
	return y
}

func (r *Holder) writes(v string) {
	r.Val.X = 1
	r.Ptr.X = 1
	r.Sub.Ptr.X = 1
	r.Users[r.Key] = v
	r.N += 1
	r.N = r.N + r.Step
	r.Items = append(r.Items, 1)
}

func (r *Holder) addresses() {
	n := &r.N
	x := &r.Val.X
	p := &r.Ptr.X
	_, _, _ = n, x, p
	_ = &shared
}

func (r *Holder) calls(w http.ResponseWriter, req *http.Request) {
	r.Ptr.Bump()
	r.Val.Bump()
	r.Val.Value()
	r.H.ServeHTTP(w, req)
	r.OnErr(nil)
	r.mu.Lock()
	r.Touch()
	shared.Touch()
	OnReady()
	f := r.Val.Bump
	g := r.Ptr.Bump
	f()
	g()
}

func lock(l *Locked) {
	l.Lock()
}

func (r *Holder) outsides(req *http.Request) int {
	n := maxUsers
	n += http.StatusOK
	n += len(os.Args)
	n += len(req.Method + req.URL.Path)
	const local = 2
	return n + local
}

func (r *Holder) hands(other *Holder, ch chan []int) map[int]string {
	use(sum(r.Items))
	m := r.Users
	other.Items = r.Items
	_ = Holder{Items: r.Items}
	ch <- r.Items
	keep(r.Ptr)
	use(sum(r.Items[1:]))
	_ = m
	return r.Users
}

func bagLen(b *Bag[int]) int { return len(b.items) }

// Escapes through struct values reach every enclosing field, up to the first pointer.
type Outer struct {
	In Holder
	L  Locked
}

func (o *Outer) nested() {
	_ = &o.In.Val.X
	o.In.Val.Bump()
	o.L.Lock()
	_ = &o.In.ID
	_ = &shared.Val.X
	_ = &o.In.Ptr.X
}

// Consts in types are read wherever a type is written: signatures, var types, literals.
func sized(b [maxUsers]byte) [maxUsers]byte {
	var c [maxUsers]byte
	_ = func(d [maxUsers]byte) {}
	_ = c
	return [maxUsers]byte{}
}

var table [maxUsers]int
