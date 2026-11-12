package main

import "example.com/dep"

// An interface defined from another named interface, in a repo package: its
// methods are dep.Greeter's. A repo package's row holds no export data, so
// unlike dep/named it's cached.
type loud dep.Greeter

func shoutAll(l loud) string { return l.Greet("ALL") }
