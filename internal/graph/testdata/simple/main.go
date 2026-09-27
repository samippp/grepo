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
