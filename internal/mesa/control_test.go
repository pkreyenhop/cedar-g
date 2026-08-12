package mesa

import (
	"strings"
	"testing"
)

// TestSelectExpr covers SELECT used in expression position with equality and
// open (relational) guards and an ENDCASE default.
func TestSelectExpr(t *testing.T) {
	src := `Foo: CEDAR PROGRAM = {
  Name: PROC [n: INTEGER] RETURNS [Rope.ROPE] = {
    RETURN[SELECT n FROM
      1 => "one",
      2, 3 => "few",
      > 9 => "many",
      ENDCASE => "some"];
  };
  IO.PutRope[Name[1]];  IO.PutChar[32];
  IO.PutRope[Name[3]];  IO.PutChar[32];
  IO.PutRope[Name[5]];  IO.PutChar[32];
  IO.PutRope[Name[42]];
}.`
	if got := runProg(t, src); got != "one few some many" {
		t.Fatalf("got %q, want %q", got, "one few some many")
	}
}

// TestErrorRaiseCatch covers ERROR raising and both an ENABLE block handler and
// a statement-level "! …" catch clause.
func TestErrorRaiseCatch(t *testing.T) {
	src := `Foo: CEDAR PROGRAM = {
  Trouble: ERROR = CODE;
  Risky: PROC = { ERROR Trouble };
  {
    ENABLE Trouble => { IO.PutRope["caught-block "] };
    Risky[];
    IO.PutRope["unreachable "];
  };
  Risky[ ! Trouble => IO.PutRope["caught-bang"] ];
}.`
	got := runProg(t, src)
	if !strings.Contains(got, "caught-block") || !strings.Contains(got, "caught-bang") ||
		strings.Contains(got, "unreachable") {
		t.Fatalf("unexpected: %q", got)
	}
}

// TestUncaughtError checks that an uncaught ERROR aborts with a clear message
// rather than silently continuing.
func TestUncaughtError(t *testing.T) {
	src := `Foo: CEDAR PROGRAM = {
  Boom: ERROR = CODE;
  ERROR Boom;
}.`
	m, err := ParseSource(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if runErr := NewInterp(&strings.Builder{}).Run(m); runErr == nil ||
		!strings.Contains(runErr.Error(), "Boom") {
		t.Fatalf("expected uncaught Boom error, got: %v", runErr)
	}
}
