package mesa

import (
	"bytes"
	"strings"
	"testing"
)

// TestOpaqueImports checks that a module wiring together an unmodeled interface
// elaborates: references to Commander.Register etc. become opaque no-ops rather
// than "undefined identifier" failures.
func TestOpaqueImports(t *testing.T) {
	src := `FooImpl: CEDAR PROGRAM
  IMPORTS Commander, ViewerOps, Rope
  EXPORTS Foo = {
  handle: ViewerOps.Viewer ← ViewerOps.CreateViewer[name: "foo"];
  greeting: Rope.ROPE ← Rope.Cat["hi ", "there"];  -- a modeled interface still works
  Commander.Register["foo", FooProc, "does foo"];
  IF handle = NIL THEN IO.PutRope[greeting];       -- opaque handle reads as NIL

  FooProc: PROC = { IO.PutRope["!"] };
}.`
	got := runProg(t, src)
	if got != "hi there!" && got != "hi there" {
		t.Fatalf("got %q", got)
	}
}

// TestEntryProc checks the entry model: a library module with no side-effecting
// main body can still be run by invoking one of its exported procedures.
func TestEntryProc(t *testing.T) {
	src := `FooImpl: CEDAR PROGRAM EXPORTS Foo = {
  Greet: PROC [who: Rope.ROPE] RETURNS [Rope.ROPE] = {
    RETURN[Rope.Cat["Hello, ", who]];
  };
}.`
	m, err := ParseSource(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	in := NewInterp(&buf)
	if err := in.Run(m); err != nil {
		t.Fatalf("run: %v", err)
	}
	out, err := in.CallProc("Greet", "world")
	if err != nil {
		t.Fatalf("CallProc: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, "Hello, world") {
		t.Fatalf("got %v", out)
	}
}
