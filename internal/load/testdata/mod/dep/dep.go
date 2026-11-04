// Package dep is a dependency outside the main module: only its declarations
// matter, its bodies are skipped.
package dep

import "strings"

// Box is generic, so instances in the main module carry dep's type params.
type Box[T any] struct{ V T }

func (b Box[T]) Get() T { return b.V }

type Greeter interface{ Greet(name string) string }

type Plain struct{ Prefix string }

func (p Plain) Greet(name string) string { return strings.TrimSpace(p.Prefix + " " + name) }

// Size is a const whose value the main module reads.
const Size = 4 << 10
