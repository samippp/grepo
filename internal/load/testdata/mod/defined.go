package main

import (
	"example.com/dep/named"
	"example.com/dep/wrap"
)

// Calls through interfaces defined from other named interfaces: the methods
// are dep.Greeter's and named.Getter[int]'s, as the type checker gives them.
func greetAll(h named.Hello, g named.IntGetter) (string, int) { return h.Greet("all"), g.Get() }

func viaWrap() (string, int) { return wrap.Hi().Greet("x"), wrap.Ints().Get() }
