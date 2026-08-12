package mesa

import "testing"

// TestNumberLiterals covers Mesa's number forms that earlier tripped the lexer:
// hexadecimal without an H suffix (leading 0), octal with a scale factor, and the
// ordinary H/B/C/D markers.
func TestNumberLiterals(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"0FF", 0xFF},             // leading-0 hex, no H suffix
		{"0c3d2e1f0", 0xC3D2E1F0}, // SHA-1 constant, leading-0 hex, no H
		{"0FFH", 0xFF},            // explicit hex
		{"377B", 0xFF},            // octal
		{"144B2", 6400},           // octal 144 scaled by 8^2 == 100*64 == 6400
		{"14400B", 6400},          // plain octal, same value
		{"255", 255},              // decimal
		{"10D", 10},               // explicit decimal
	}
	for _, c := range cases {
		toks, err := NewLexer(c.src).Tokenize()
		if err != nil {
			t.Errorf("%s: lex error: %v", c.src, err)
			continue
		}
		if toks[0].Kind != TInt || toks[0].Int != c.want {
			t.Errorf("%s: got kind=%v int=%d, want %d", c.src, toks[0].Kind, toks[0].Int, c.want)
		}
	}
}

// TestTightCommentFragment checks that a line-start "--Foo,--" comment is bounded
// (real code follows on the line), while a prose "-- ... -- ..." line-start
// comment still runs to end of line.
func TestTightCommentFragment(t *testing.T) {
	// A commented-out DIRECTORY entry followed by a live one and a terminator.
	toks, err := NewLexer("--BackStop,-- Rope;").Tokenize()
	if err != nil {
		t.Fatalf("lex error: %v", err)
	}
	// Expect: Rope ; EOF  (the comment fragment is skipped, ';' survives).
	if len(toks) != 3 || toks[0].Text != "Rope" || toks[1].Text != ";" {
		t.Fatalf("got %d tokens %+v, want [Rope ; EOF]", len(toks), toks)
	}

	// A prose comment whose text contains '--' stays a full-line comment.
	toks, err = NewLexer("-- see also -- the other file\nx").Tokenize()
	if err != nil {
		t.Fatalf("lex error: %v", err)
	}
	if len(toks) != 2 || toks[0].Text != "x" {
		t.Fatalf("prose comment leaked code: %+v", toks)
	}
}

// TestEndDotTruncation checks that trailing free text after the module
// terminator "END." is ignored, even when it contains characters that would not
// lex as code.
func TestEndDotTruncation(t *testing.T) {
	src := "Foo: PROGRAM = BEGIN END.\nRe: was it worth it? no -- stray ? and % junk\n"
	if _, err := ParseSource(src); err != nil {
		t.Fatalf("trailing text after END. should be ignored, got: %v", err)
	}
}

// TestNulInString checks that a NUL byte inside a string literal is content, not
// end-of-input (Cedar Rope constants sometimes embed 0B).
func TestNulInString(t *testing.T) {
	src := "Foo: PROGRAM = { s: STRING ~ \"a\x00b\"; }."
	if _, err := ParseSource(src); err != nil {
		t.Fatalf("NUL in string should lex, got: %v", err)
	}
}
