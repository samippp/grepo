package store

import "example.com/simple/model"

// A repo const from another package is read, like one from this package.
func tagLimit() int { return model.MaxTag }
