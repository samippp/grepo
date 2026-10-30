package store

import "example.com/simple/model"

// Fake's seal is store's, a different method from model's: not Sealed.
type Fake struct{}

func (Fake) seal() {}

// Sealer gets model's seal from the embedded Token: Sealed.
type Sealer struct{ model.Token }

// Finder writes Find with int and another param name: still Lookup.
type Finder struct{}

func (Finder) Find(n int) model.User { return model.User{} }

// Variadic and slice params differ: varLog is a Logger, sliceLog isn't.
type Logger interface{ Log(args ...string) }

type varLog struct{}

func (varLog) Log(xs ...string) {}

type sliceLog struct{}

func (sliceLog) Log(xs []string) {}

// Both functions declare a result and an interface returning it. Both print
// as store.result, but they're different types: impl is a maker, not an other.
func makers() {
	type result struct{}
	type maker interface{ Make() result }
	type impl struct{ maker }
}

func others() {
	type result struct{}
	type other interface{ Make() result }
}

// byte is uint8: rawWriter is a Writer.
type Writer interface{ Write(p []uint8) }

type rawWriter struct{}

func (rawWriter) Write(p []byte) {}
