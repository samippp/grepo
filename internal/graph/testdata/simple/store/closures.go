package store

import (
	"net/http"
	"sort"
)

// Closures: nodes in TestCallGraphClosures, their calls and writes in the other tests.

type Runner struct {
	OnDone func()
	Run    func() error
}

func (r *Runner) start() {}

func (r *Runner) serve(w http.ResponseWriter, req *http.Request) {}

func handle(w http.ResponseWriter, req *http.Request) {}

func double[T any](x T) T { return x }

func mapAll[T any](xs []T, f func(T) T) {
	for i := range xs {
		func() { xs[i] = f(xs[i]) }()
	}
}

func (r *Runner) closures(xs []string) {
	func() {
		func() {}()
	}()
	inc := func() {}
	inc()
	go r.start()
	defer r.start()
	go func() { r.start() }()
	defer func() {}()
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	http.HandleFunc("/a", handle)
	http.HandleFunc("/b", r.serve)
	mapAll(xs, double[string])
	r.OnDone = func() {}
	r.OnDone = r.start
	_ = Runner{Run: func() error { return nil }}
	r.OnDone()
}

var hooks, fallback = func() {}, func() {}

var _ = func() int { return 1 }()

var nested = func() {
	func() {}()
}
