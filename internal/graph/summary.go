// Type summaries: what implementation matching needs from a package, stored
// with it so a cached package needs no type-checking.

package graph

// typeSummary is one package's types by method set. Maps go from method key
// (unexported names qualified by package) to signature key; equal keys mean
// identical methods. No generic types or empty interfaces.
type typeSummary struct {
	Concrete   []concreteSummary `json:"concrete"`
	Interfaces []ifaceSummary    `json:"interfaces"`
}

type concreteSummary struct {
	ID             string            `json:"id"`
	ValueMethods   map[string]string `json:"valueMethods"`   // T's
	PointerMethods map[string]string `json:"pointerMethods"` // *T's, a superset
}

type ifaceSummary struct {
	ID      string            `json:"id"`
	Methods map[string]string `json:"methods"` // embedded ones included
}

// matchImplementations finds implementations across all summaries, as
// implementations() does from type-checker objects.
func matchImplementations(summaries []typeSummary) []Implementation {
	return []Implementation{} // not written yet
}
