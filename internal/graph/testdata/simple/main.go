// Command simple is a test fixture for grepo, not real code.
//
// Shape: main → store → model. model.User crosses a package boundary
// through store.Get, the kind of data flow M1 should detect.
package main

import (
	"fmt"

	"example.com/simple/store"
)

func main() {
	s := store.New()
	fmt.Println(s.Get(1))
}
