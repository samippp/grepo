package graph

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/samippp/grepo/internal/load"
)

// A package with type errors is still walked: what the checker couldn't
// resolve has no type or object, and mustn't crash the walk.
func TestTypeErrors(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod": "module example.com/broken\n\ngo 1.22\n",
		"broken.go": `package broken

type S struct{ x int }

var o struct{ y int }

var v = S{o.y: 1} // a field key that isn't a name

func f() {
	undefinedMap["a"] = 1
	delete(missing, 1)
	*nowhere = 2
	copy(gone, nil)
	clear(absent)
	nothing.field = 3
	nothing.field.deeper++
	_ = &unknown
	unknown.Method()
	go undefinedFunc()
	for k = range undefinedSlice {
	}
}
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cb := NewCallGraphBuilder(dir)
	res, err := load.Load(t.Context(), dir, nil, slog.New(slog.DiscardHandler), cb.Add)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Packages) != 1 || len(res.Packages[0].Errors) == 0 {
		t.Fatalf("want one package with type errors, got %d packages", len(res.Packages))
	}
	if _, _, err := cb.CallGraph(); err != nil {
		t.Fatal(err)
	}
}
