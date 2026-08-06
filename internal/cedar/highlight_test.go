package cedar

import "testing"

// catAt returns the category of the span covering the given row and column, and
// whether such a span exists.
func catAt(spans []Span, row, col int) (Category, bool) {
	for _, s := range spans {
		if s.Row == row && col >= s.Col && col < s.Col+s.Len {
			return s.Cat, true
		}
	}
	return 0, false
}

func TestHighlightBasics(t *testing.T) {
	builtins := map[string]bool{"INTEGER": true}
	src := "Count: INTEGER _ 42;"
	//      0123456789...
	spans := Highlight(src, builtins)

	// "INTEGER" is a builtin => type.
	if c, ok := catAt(spans, 0, 7); !ok || c != C_Type {
		t.Errorf("INTEGER: got cat=%d ok=%v, want C_Type", c, ok)
	}
	// "42" => number.
	if c, ok := catAt(spans, 0, 17); !ok || c != C_Num {
		t.Errorf("42: got cat=%d ok=%v, want C_Num", c, ok)
	}
	// ":" => operator.
	if c, ok := catAt(spans, 0, 5); !ok || c != C_Op {
		t.Errorf("colon: got cat=%d ok=%v, want C_Op", c, ok)
	}
	// "Count" => plain identifier.
	if c, ok := catAt(spans, 0, 0); !ok || c != C_Ident {
		t.Errorf("Count: got cat=%d ok=%v, want C_Ident", c, ok)
	}
}

func TestHighlightKeyword(t *testing.T) {
	spans := Highlight("RETURN[x]", nil)
	if c, ok := catAt(spans, 0, 0); !ok || c != C_Kw {
		t.Errorf("RETURN: got cat=%d ok=%v, want C_Kw", c, ok)
	}
}

func TestHighlightMultiLineComment(t *testing.T) {
	// A "<<" without a closing ">>" on the same line must colour the following
	// lines as comment until ">>" appears.
	src := "a << open\nstill comment\nclose >> b"
	spans := Highlight(src, nil)

	if c, ok := catAt(spans, 1, 0); !ok || c != C_Cmt {
		t.Errorf("continuation line: got cat=%d ok=%v, want C_Cmt", c, ok)
	}
	// After ">>" on line 2 (0-based), "b" (rune index 9) is code again.
	if c, ok := catAt(spans, 2, 9); !ok || c != C_Ident {
		t.Errorf("post-comment identifier: got cat=%d ok=%v, want C_Ident", c, ok)
	}
}

// TestHighlightColumnsWithArrow verifies that a single-byte Latin-1 "←" (0xAC)
// keeps span columns aligned with rune-cell positions.
func TestHighlightColumnsWithArrow(t *testing.T) {
	// Byte 0xAC decodes to the rune '←'; "x ← 42" is 6 runes.
	src := "x ¬ 42" // Go source: use the Latin-1 arrow byte via ¬
	spans := Highlight(src, nil)
	// "42" starts at rune index 4.
	if c, ok := catAt(spans, 0, 4); !ok || c != C_Num {
		t.Errorf("42 after arrow: got cat=%d ok=%v, want C_Num", c, ok)
	}
}
