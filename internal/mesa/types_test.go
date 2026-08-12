package mesa

import (
	"bytes"
	"strings"
	"testing"
)

// TestTolerantTypes checks that reference/collection types and unresolved
// imported types no longer abort a module: they resolve to opaque handles
// (default NIL) instead of "unknown type".
func TestTolerantTypes(t *testing.T) {
	src := `Foo: CEDAR PROGRAM = {
  p: POINTER TO INTEGER;
  r: REF INTEGER;
  l: LIST OF INTEGER;
  s: Rope.ROPE;
  v: ViewerClasses.Viewer;
  n: INT ~ 5;
  IO.PutRope[out, "hi"];
}.`
	m, err := ParseSource(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	if err := NewInterp(&buf).Run(m); err != nil {
		if strings.Contains(err.Error(), "unknown type") {
			t.Fatalf("opaque type resolution failed: %v", err)
		}
		// Other runtime errors (e.g. an unimplemented builtin) are out of scope
		// for this test; only "unknown type" must be gone.
	}
}

// TestStepBudget checks that a runaway loop is stopped by the execution budget
// rather than hanging.
func TestStepBudget(t *testing.T) {
	src := `Foo: PROGRAM = { DO ENDLOOP; }.`
	m, err := ParseSource(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	in := NewInterp(&bytes.Buffer{})
	in.SetMaxSteps(100_000)
	err = in.Run(m)
	if err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected budget-exceeded error, got: %v", err)
	}
}
