package mesa

import "testing"

// TestRopeAndConvert exercises the Rope and Convert interfaces and IO.PutF
// formatting the way real Cedar text programs use them.
func TestRopeAndConvert(t *testing.T) {
	src := `Foo: CEDAR PROGRAM = {
  greeting: Rope.ROPE ← Rope.Cat["Hello", ", ", "world"];
  IO.PutF["%g (len %g)\n", greeting, Rope.Length[greeting]];
  n: INT ← Convert.IntFromRope["42"];
  IO.PutRope[Convert.RopeFromInt[n * 2]];
  IO.PutChar[10];
  IF Rope.Equal["abc", "ABC", FALSE] THEN IO.PutRope["equalfold\n"];
  IO.PutRope[Rope.Substr["abcdef", 2, 3]];
}.`
	got := runProg(t, src)
	want := "Hello, world (len 12)\n84\nequalfold\ncde"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestPutFR checks that PutFR formats to a ROPE and rope-capture streams work.
func TestPutFR(t *testing.T) {
	src := `Foo: CEDAR PROGRAM = {
  r: Rope.ROPE ← IO.PutFR["x=%g y=%g", 3, 4];
  IO.PutRope[r];
}.`
	if got := runProg(t, src); got != "x=3 y=4" {
		t.Fatalf("got %q, want %q", got, "x=3 y=4")
	}
}
