// Command simple is a test fixture for grepo, not real code.
//
// Shape: main → store → model. model.User crosses a package boundary
// through store.Get, the kind of data flow the call graph should detect.
// main also contains one example of each kind of call the call graph has to
// classify; see callgraph_test.go for what each one should produce.
package main

import (
	"fmt"

	"example.com/simple/store"
)

func main() {
	s := store.New()
	u := s.Get(1)

	var g store.Getter = s
	g.Get(2)

	f := greet
	f(u.Name)

	func() {
		greet("closure")
	}()

	n := len(u.Name)
	_ = int64(n)

	fmt.Println(u)
}

func greet(name string) {
	fmt.Println("hello", name)
}

func first[T any](xs []T) T { return xs[0] }

func pair[K, V any](k K, v V) {}

// generics calls generic funcs with explicit type args, plus an indexed func value.
func generics() {
	xs := []int{1, 2}
	first[int](xs)
	pair[string, int]("a", 1)

	handlers := []func(string){greet}
	handlers[0]("indexed")
}

// List is generic; useList calls its method through List[int] (#5).
type List[T any] struct{ items []T }

func (l *List[T]) Push(v T) { l.items = append(l.items, v) }

func useList() {
	var l List[int]
	l.Push(1)
}

// Two inits in one file (#6), plus a method named init that keeps its normal ID.
func init() { greet("init 1") }

func init() { greet("init 2") }

func (l *List[T]) init() {}
