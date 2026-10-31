package model

// Implementations matched from stored signatures must agree with
// types.Implements. store/impls.go has the other side of each case.

// Sealed has an unexported method: only types with model's own seal
// implement it, whatever their package.
type Sealed interface{ seal() }

type Token struct{}

func (Token) seal() {}

// Lookup's signature uses the alias ID: identical to one written with int.
type Lookup interface{ Find(id ID) User }

// Stringish only constrains type params: no implementations, though UserID
// is in its type set.
type Stringish interface {
	~int
	String() string
}
