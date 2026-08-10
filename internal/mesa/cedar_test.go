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
		"errors/signals as types and statements": `
			Foo: CEDAR DEFINITIONS ~ {
			  Error: ERROR [why: INT];
			  Warn: SIGNAL ANY RETURNS ANY;
			  Raise: PROC ~ { ERROR Error[why: 3]; SIGNAL Warn };
			};`,
		"inline/entry proc bodies and keyword args": `
			FooImpl: CEDAR MONITOR ~ {
			  Len: ENTRY PROC [r: Rope.ROPE] RETURNS [n: INT ← 0] = INLINE {
			    RETURN [r.Length[]] };
			  Go: PROC ~ { x: INT ← f[key: 1, other~2, , 3]; };
			};`,
		"variant records and WITH SELECT": `
			Foo: CEDAR DEFINITIONS ~ {
			  Node: TYPE ~ RECORD [
			    name: Rope.ROPE,
			    body: SELECT tag: * FROM
			      leaf => [value: INT],
			      inner => [kids: LIST OF Node],
			      ENDCASE ];
			  Walk: PROC [n: REF Node] ~ {
			    WITH n SELECT FROM
			      x: INT => RETURN,
			      ENDCASE => NULL;
			  };
			};`,
		"select expr, relational guards, IN, loops": `
			FooImpl: CEDAR PROGRAM ~ {
			  Classify: PROC [c: CHAR] RETURNS [INT] ~ {
			    SELECT c FROM
			      < '0, > '9 => RETURN [0];
			      IN ['0..'9] => RETURN [1];
			      ENDCASE => RETURN [2];
			  };
			  Sum: PROC ~ {
			    total: INT ← 0;
			    FOR i: NAT DECREASING IN [0..10) DO total ← total + i; ENDLOOP;
			    IF total NOT IN [1..5] THEN total ← 0;
			  };
			};`,
		"literals: octal, hex, char escapes, long string, machine record": `
			Foo: CEDAR DEFINITIONS ~ {
			  mask: CARD ~ 377B + 0FFH;
			  nl: CHAR ~ '\n;
			  bs: CHAR ~ '\\;
			  del: CHAR ~ 177C;
			  msg: Rope.ROPE ~ "hello"L;
			  Packed: TYPE ~ MACHINE DEPENDENT RECORD [
			    op (0: 0..5): NAT, flag (0: 6..6): BOOL ];
			};`,
		"full-line comment with inner -- vs inline comment": `
			-- $Tioga -- sends a list of events -- all one comment
			Foo: CEDAR DEFINITIONS ~ {
			  n: INT ~ 0 --inline-- + 1;  -- $Gargoyle -- trailing
			};`,
		"bracketless return, NARROW type args, power, with-expr value": `
			FooImpl: CEDAR PROGRAM ~ {
			  Cmp: PROC [a, b: REF ANY] RETURNS [INT] ~ {
			    RETURN Basics.CompareInt[NARROW[a, REF INT]^, NARROW[b, REF INT]^] };
			  bits: INT ~ 2 ** 10;
			  rope: ROPE ~ WITH prop SELECT FROM r: ROPE => r, ENDCASE => NIL;
			};`,
		"tilde aggregates, := assign, D-scaled, bare RETURN, PROGRAM, relative ptr": `
			FooImpl: CEDAR PROGRAM ~ {
			  Sched: TYPE ~ RECORD [
			    p: PageOffset,
			    fault: SIGNAL [dest: PROGRAM] ];
			  PageOffset: TYPE ~ BasePtr RELATIVE ORDERED POINTER TO Entry;
			  rec: Schedule ← [date~nullGMT, permittedDumps~ALL[[size~0, days~3]]];
			  Go: PROC [x: INT] RETURNS [INT] ~ {
			    pause: INT ~ 1D3;
			    x := x + 1;
			    IF x = 0 THEN RETURN ELSE RETURN x;
			  };
			};`,
		"exits, repeat, goto, fork, new[type[n]]": `
			FooImpl: CEDAR PROGRAM ~ {
			  Go: PROC [n: NAT] RETURNS [REF Seq] ~ {
			    s: REF Seq ← NEW[Seq[n]];
			    DO IF n = 0 THEN GO TO done; n ← n - 1; REPEAT done => EXIT; ENDLOOP;
			    Process.Detach[FORK Worker[s]];
			    RETURN [s ! ABORTED => CONTINUE];
			    EXITS oops => RETURN [NIL];
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
