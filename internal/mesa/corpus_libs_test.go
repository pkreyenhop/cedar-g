package mesa

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"cedarg/internal/tioga"
)

// These tests exercise real Cedar library modules from the download-src corpus:
// they load the actual source, elaborate it, and then call its exported
// procedures through the entry model (Interp.CallProc), checking the results.
// This shows the interpreter running genuine Xerox Cedar library code, not just
// the hand-written samples.

// findCorpusFile locates a named .mesa file under the download-src corpus,
// searching the usual relative locations and $CEDAR_DIR.
func findCorpusFile(t *testing.T, name string) string {
	t.Helper()
	roots := []string{"../../download-src", "download-src"}
	if d := os.Getenv("CEDAR_DIR"); d != "" {
		roots = append([]string{d}, roots...)
	}
	for _, root := range roots {
		var found string
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && filepath.Base(p) == name {
				found = p
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	t.Skipf("%s not found under the download-src corpus (set CEDAR_DIR)", name)
	return ""
}

// loadCorpusModule reads a corpus .mesa file (a Tioga container), decodes and
// elaborates it, and returns a ready interpreter for calling its procedures.
func loadCorpusModule(t *testing.T, name string) *Interp {
	t.Helper()
	data, err := os.ReadFile(findCorpusFile(t, name))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseSource(tioga.Read(data, true).Code)
	if err != nil {
		t.Fatalf("%s: parse: %v", name, err)
	}
	in := NewInterp(&bytes.Buffer{})
	if err := in.Run(m); err != nil {
		t.Fatalf("%s: elaborate: %v", name, err)
	}
	return in
}

// list builds a LIST OF REF ANY (the corpus's LORA type) from integer elements.
func list(vs ...int64) *Cons {
	var h *Cons
	for k := len(vs) - 1; k >= 0; k-- {
		h = &Cons{First: vs[k], Rest: h}
	}
	return h
}

// call invokes an exported procedure and fails on any runtime error.
func call(t *testing.T, in *Interp, proc string, args ...any) any {
	t.Helper()
	r, err := in.CallProc(proc, args...)
	if err != nil {
		t.Fatalf("%s: %v", proc, err)
	}
	return r
}

// TestCorpusListImpl loads Cedar's real List implementation (ListImpl.mesa) and
// exercises its list-algebra procedures.
func TestCorpusListImpl(t *testing.T) {
	in := loadCorpusModule(t, "ListImpl.mesa")

	l := list(10, 20, 30, 40)
	a := list(1, 2, 3)
	b := list(2, 3, 4)

	cases := []struct {
		proc string
		args []any
		want string
	}{
		{"Length", []any{l}, "4"},
		{"Reverse", []any{l}, "(40 30 20 10)"},
		{"Memb", []any{int64(30), l}, "TRUE"},
		{"Memb", []any{int64(99), l}, "FALSE"},
		{"Append", []any{a, b}, "(1 2 3 2 3 4)"},
		{"Nconc", []any{list(1, 2), list(3)}, "(1 2 3)"},
		{"EqLists", []any{a, a}, "TRUE"},
		{"EqLists", []any{a, b}, "FALSE"},
		{"Union", []any{a, b}, "(1 2 3 4)"},
		{"Intersection", []any{a, b}, "(3 2)"},
		{"ListDifference", []any{a, b}, "(1)"},
		{"Remove", []any{int64(20), l}, "(10 30 40)"},
		{"NthTail", []any{l, int64(2)}, "(30 40)"},
		{"NthElement", []any{l, int64(1)}, "10"},
		{"NthElement", []any{l, int64(4)}, "40"},
	}
	for _, c := range cases {
		got := FormatValue(call(t, in, c.proc, c.args...))
		if got != c.want {
			t.Errorf("List.%s -> %s, want %s", c.proc, got, c.want)
		}
		t.Logf("List.%-14s -> %s", c.proc, got)
	}
}
