package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is the graph package's test repo; printed paths are relative to
// this package's directory, where tests run.
var fixture = filepath.Join("..", "graph", "testdata", "simple")

// run runs grepo with args, as main does, and returns its stdout, stderr and
// exit code.
func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := newRootCmd(&app{})
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errOut)
	err := root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), ExitCode(err)
}

func TestCallers(t *testing.T) {
	at := func(file string) string { return filepath.Join(fixture, filepath.FromSlash(file)) }
	for _, tc := range []struct {
		args []string
		want []string // stdout lines
		code int
	}{
		// A direct call, then one through the interface *Store implements.
		{[]string{"Store.Get"}, []string{
			at("main.go:17") + ": main.main: u := s.Get(1)",
			at("main.go:20") + ": main.main: g.Get(2)  [via store.Getter]",
		}, 0},
		{[]string{"--direct", "Store.Get"}, []string{at("main.go:17") + ": main.main: u := s.Get(1)"}, 0},
		// Passed as a value.
		{[]string{"handle"}, []string{
			at("store/closures.go:40") + `: (*store.Runner).closures: http.HandleFunc("/a", handle)  [passed to http.HandleFunc]`,
		}, 0},
		// Through interfaces from outside the repo: the built-in error, and io.Writer.
		{[]string{"failure.Error"}, []string{
			at("main.go:78") + ": main.describe: func describe(err error) string { return err.Error() }  [via error]",
		}, 0},
		{[]string{"bufWriter.Write"}, []string{
			at("store/outside.go:12") + ": store.flush: func flush(w io.Writer) { w.Write(nil) }  [via io.Writer]",
		}, 0},
		// A function outside the repo.
		{[]string{"fmt.Println"}, []string{
			at("main.go:32") + ": main.main: fmt.Println(u)",
			at("main.go:36") + `: main.greet: fmt.Println("hello", name)`,
		}, 0},
		// Found, but nothing calls it: 1, as grep's.
		{[]string{"main.main"}, nil, 1},
		// Not found: an error.
		{[]string{"Nope"}, nil, 2},
	} {
		stdout, stderr, code := run(t, append([]string{"callers"}, append(tc.args, fixture)...)...)
		got := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
		if stdout == "" {
			got = nil
		}
		if code != tc.code || strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("callers %v: exit %d, stdout:\n%s\nwant exit %d:\n%s\nstderr: %s",
				tc.args, code, stdout, tc.code, strings.Join(tc.want, "\n"), stderr)
		}
	}
}

// An ambiguous name lists its candidates on stderr, each with a name that
// picks it, and exits with 2; the package in the directory given wins.
func TestCallersAmbiguous(t *testing.T) {
	stdout, stderr, code := run(t, "callers", "seal", fixture)
	want := "seal matches 3 functions; name one of them:\n" +
		filepath.Join(fixture, "model", "impls.go") + ":8: (model.Sealed).seal\n" +
		filepath.Join(fixture, "model", "impls.go") + ":12: (model.Token).seal\n" +
		filepath.Join(fixture, "store", "impls.go") + ":8: (store.Fake).seal\n"
	if code != 2 || stdout != "" || stderr != want {
		t.Errorf("callers seal: exit %d, stdout %q, stderr:\n%s\nwant exit 2, stderr:\n%s", code, stdout, stderr, want)
	}

	// From the store directory, store's seal is the one meant. Nothing calls
	// it, so --json still names it, with no sites, and exits with 1.
	stdout, stderr, code = run(t, "callers", "--json", "seal", filepath.Join(fixture, "store"))
	var out callersJSON
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("callers --json seal: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if code != 1 || out.Function.ID != "(example.com/simple/store.Fake).seal" || out.Sites == nil || len(out.Sites) != 0 {
		t.Errorf("callers --json seal from store: exit %d, function %s, sites %v; want exit 1, (store.Fake).seal, []",
			code, out.Function.ID, out.Sites)
	}
}

func TestCallersJSON(t *testing.T) {
	stdout, stderr, code := run(t, "callers", "--json", "Store.Get", fixture)
	var out callersJSON
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("callers --json: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if code != 0 || out.Function.ID != "(*example.com/simple/store.Store).Get" ||
		out.Function.Pos != filepath.Join(fixture, "store", "store.go")+":23" {
		t.Errorf("function = %+v, exit %d", out.Function, code)
	}
	if len(out.Sites) != 2 {
		t.Fatalf("sites = %+v, want 2", out.Sites)
	}
	direct, iface := out.Sites[0], out.Sites[1]
	if direct.Kind != "direct" || direct.Pos != filepath.Join(fixture, "main.go")+":17" ||
		direct.From != "example.com/simple.main" || direct.Line != "u := s.Get(1)" ||
		direct.CallKind != "static" || len(direct.Args) != 1 {
		t.Errorf("direct site = %+v", direct)
	}
	if iface.Kind != "interface" || iface.Via != "example.com/simple/store.Getter" ||
		iface.Callee != "(example.com/simple/store.Getter).Get" || iface.CallKind != "interface" {
		t.Errorf("interface site = %+v", iface)
	}
}
