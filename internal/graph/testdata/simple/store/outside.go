package store

import "io"

// An interface from outside the repo counts once the repo calls through it:
// flush's w.Write makes bufWriter an io.Writer. rawWriter's Write has no
// results, so it isn't one.
type bufWriter struct{}

func (*bufWriter) Write(p []byte) (int, error) { return len(p), nil }

func flush(w io.Writer) { w.Write(nil) }

// Built in, and main's describe calls err.Error(): failure is an error.
type failure struct{}

func (failure) Error() string { return "failed" }
