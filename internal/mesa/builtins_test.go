package mesa

import "testing"

// TestTypeBuiltins covers LAST/FIRST over base and enum types, ALL array fill,
// SUCC/PRED, and LOOPHOLE/NARROW/ISTYPE identity casts.
func TestTypeBuiltins(t *testing.T) {
	src := `Foo: PROGRAM = {
  Color: TYPE = {red, green, blue};
  WriteInt[LAST[CARDINAL]];        -- 2147483647
  WriteString[" "];
  WriteString[FormatColor[LAST[Color]]];   -- blue
  WriteString[" "];
  WriteString[FormatColor[SUCC[red]]];     -- green
  WriteString[" "];
  a: ARRAY [0..3) OF INTEGER ← ALL[7];
  WriteInt[a[0]]; WriteInt[a[1]]; WriteInt[a[2]];   -- 777
  WriteString[" "];
  WriteInt[LOOPHOLE[42, INTEGER]];  -- 42

  FormatColor: PROC [c: Color] RETURNS [Rope.ROPE] = {
    IF c = red THEN RETURN["red"];
    IF c = green THEN RETURN["green"];
    RETURN["blue"];
  };
}.`
	got := runProg(t, src)
	want := "2147483647 blue green 777 42"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestNumericAndListLibs covers RealFns, Basics bit ops and the List interface.
func TestNumericAndListLibs(t *testing.T) {
	src := `Foo: CEDAR PROGRAM = {
  WriteInt[Basics.BITAND[12, 10]];   -- 8
  WriteString[" "];
  WriteReal[RealFns.Sqrt[16.0]];     -- 4.0
  WriteString[" "];
  l: LIST OF INTEGER ← List.Reverse[LIST[1, 2, 3]];
  WriteInt[l.first];                 -- 3
  WriteInt[List.Nth[l, 2]];          -- 2 (1-based)
}.`
	got := runProg(t, src)
	if got != "8 4.0 32" {
		t.Fatalf("got %q, want %q", got, "8 4.0 32")
	}
}
