// Package wrap only mentions named's types, so it's cached; its export data
// copies them too, and reading it must keep named's own (checked) ones.
package wrap

import "example.com/dep/named"

func Hi() named.Hello { return nil }

func Ints() named.IntGetter { return nil }
