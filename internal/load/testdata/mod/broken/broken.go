// Package broken has type errors: both loaders report them and still return
// its types.
package broken

// Ranging over an int needs go1.22; the module says go 1.21.
func count() int {
	n := 0
	for range 3 {
		n++
	}
	return n
}

func missing() int { return undefined }

// Fine despite the errors above: still typed.
type Kept struct{ N int }
