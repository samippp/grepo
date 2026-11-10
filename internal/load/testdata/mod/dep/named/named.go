// Package named defines interfaces from other named interfaces, so their
// methods are the other interface's. Export data copies the interface
// instead, and read back the methods would be Hello's and IntGetter's: the
// loader doesn't cache this package.
package named

import "example.com/dep"

type Hello dep.Greeter

type Getter[T any] interface{ Get() T }

type IntGetter Getter[int]
