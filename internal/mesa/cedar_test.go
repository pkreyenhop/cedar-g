package mesa

import (
	"strings"
	"testing"
)

// TestCedarSyntaxParses covers the Cedar surface syntax the lexer and parser
// were extended to understand.
func TestCedarSyntaxParses(t *testing.T) {
	cases := map[string]string{
		"module head ~ / CEDAR / DIRECTORY": `
			DIRECTORY Rope, Xl USING [Card32];
			Foo: CEDAR DEFINITIONS ~ {
			  Color: TYPE ~ {red, green, blue};
			};`,
		"ref/list/pointer/qualified/sequence types": `
			Foo: CEDAR DEFINITIONS ~ {
			  R: TYPE ~ REF Rec;
			  L: TYPE ~ LIST OF Rope.ROPE;
			  P: TYPE ~ POINTER TO Xl.XAtom;
			  A: TYPE ~ REF ANY;
			  Rec: TYPE ~ RECORD [tail: SEQUENCE len: NAT OF INT];
			};`,
		"record with private fields and defaults": `
			Foo: CEDAR DEFINITIONS ~ {
			  Rec: TYPE ~ RECORD [
			    event: Xl.SelectionRequestEvent,
			    refuse: BOOL ← FALSE,
			    next: PRIVATE REF ← NIL,
			    index: PRIVATE INT ← -1 ];
			};`,
		"octal, hex and char literals": `
			Foo: CEDAR DEFINITIONS ~ {
			  a: INT ~ 377B;
			  b: INT ~ 0FFH;
			  c: CHAR ~ 101C;
			};`,
		"block, inline and nested comments": `
			<<a block
			   << nested >> comment>>
			Foo: CEDAR DEFINITIONS ~ {
			  n: INT ~ 0 --inline-- + 1; -- trailing
			};`,
		"impl module with OPEN, ENABLE, TRUSTED, catch, deref, ~not": `
			DIRECTORY Xl, IO;
			FooImpl: CEDAR MONITOR IMPORTS Xl, IO EXPORTS Foo ~ {
			  OPEN Xl;
			  Run: PROC [p: REF INT] ~ {
			    ENABLE { UNWIND => CONTINUE };
			    done: BOOL ← ~TRUE;
			    TRUSTED { p^ ← 3 };
			    IF ~done THEN IO.PutRope[msg ! Error => CONTINUE];
			  };
			};`,
	}
	for name, src := range cases {
		if _, err := ParseSource(src); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestCedarProgramRuns proves the interpreter executes a Cedar-flavoured
// program (the '~' binding and '~' NOT operator), not just parses it.
func TestCedarProgramRuns(t *testing.T) {
	src := `
		Demo: CEDAR PROGRAM ~ {
		  yes: BOOL ~ ~FALSE;
		  IF yes THEN IO.PutLine["cedar runs"];
		}.`
	m, err := ParseSource(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var out strings.Builder
	if err := NewInterp(&out).Run(m); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "cedar runs") {
		t.Fatalf("output = %q", out.String())
	}
}
