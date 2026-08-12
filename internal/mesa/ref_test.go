package mesa

import (
	"bytes"
	"strings"
	"testing"
)

func runProg(t *testing.T, src string) string {
	t.Helper()
	m, err := ParseSource(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	if err := NewInterp(&buf).Run(m); err != nil {
		t.Fatalf("run: %v\noutput so far: %s", err, buf.String())
	}
	return buf.String()
}

// TestRefAllocAndDeref covers NEW allocation, ^ dereference, and mutation
// through a shared REF (a write through one alias is seen through another).
func TestRefAllocAndDeref(t *testing.T) {
	src := `Foo: PROGRAM = {
  Cell: TYPE = RECORD [n: INTEGER];
  p: REF Cell ← NEW[Cell ← [n: 5]];
  q: REF Cell ← p;          -- alias the same cell
  q.n ← 42;                 -- write through the alias (auto-deref)
  WriteInt[p^.n];           -- read through explicit deref: 42
}.`
	if got := runProg(t, src); got != "42" {
		t.Fatalf("got %q, want 42", got)
	}
}

// TestListOps covers the LIST constructor, CONS, first/rest, NIL and LENGTH.
func TestListOps(t *testing.T) {
	src := `Foo: PROGRAM = {
  l: LIST OF INTEGER ← LIST[10, 20, 30];
  WriteInt[l.first];              -- 10
  WriteInt[l.rest.first];         -- 20
  WriteInt[LENGTH[l]];            -- 3
  m: LIST OF INTEGER ← CONS[1, l];
  WriteInt[m.first];              -- 1
  WriteInt[LENGTH[m]];            -- 4
  empty: LIST OF INTEGER ← NIL;
  IF empty = NIL THEN WriteString["empty"];
}.`
	got := runProg(t, src)
	if !strings.HasPrefix(got, "1020314") || !strings.HasSuffix(got, "empty") {
		t.Fatalf("unexpected list output: %q", got)
	}
}
