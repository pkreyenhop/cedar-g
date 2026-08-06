package cedar

import "testing"

// typesOf runs the lexer over code and returns the token types produced.
func typesOf(code string) []TokenType {
	lx := NewLexer()
	var out []TokenType
	for _, t := range lx.Tokens(code) {
		out = append(out, t.Type)
	}
	return out
}

func TestAssignmentAndNumber(t *testing.T) {
	// '_' is an alias for the "←" assignment token.
	got := typesOf("x _ 42")
	want := []TokenType{Tok_n, Tok_2190, Tok_number}
	assertTypes(t, got, want)
}

func TestKeywords(t *testing.T) {
	got := typesOf("Foo: CEDAR DEFINITIONS = BEGIN")
	want := []TokenType{Tok_n, Tok_Colon, Tok_CEDAR, Tok_DEFINITIONS, Tok_Eq, Tok_BEGIN}
	assertTypes(t, got, want)
}

func TestIdentifierNotKeywordPrefix(t *testing.T) {
	// "ANDrew" must lex as one identifier, not the keyword AND + "rew".
	got := typesOf("ANDrew")
	want := []TokenType{Tok_n}
	assertTypes(t, got, want)
}

func TestStringAndChar(t *testing.T) {
	got := typesOf(`"hi" 'a`)
	want := []TokenType{Tok_string, Tok_char}
	assertTypes(t, got, want)
}

func TestOperators(t *testing.T) {
	got := typesOf(":= => <= .. ~= >>x")
	// ":" then "=", "=>", "<=", "..", "~=", then ">>" is only produced inside a
	// comment; here ">>" lexes as Tok_2Gt followed by identifier.
	want := []TokenType{
		Tok_Colon, Tok_Eq, Tok_EqGt, Tok_Leq, Tok_2Dot, Tok_TildeEq, Tok_2Gt, Tok_n,
	}
	assertTypes(t, got, want)
}

func TestLineComment(t *testing.T) {
	lx := NewLexer()
	lx.SetIgnoreComments(false)
	lx.SetPackComments(false)
	toks := lx.Tokens("x -- a trailing comment")
	if len(toks) != 2 || toks[0].Type != Tok_n || toks[1].Type != Tok_Comment {
		t.Fatalf("got %+v", toks)
	}
}

func TestBlockCommentPacked(t *testing.T) {
	lx := NewLexer()
	lx.SetIgnoreComments(false)
	lx.SetPackComments(true)
	toks := lx.Tokens("<< a comment >> y")
	if len(toks) != 2 || toks[0].Type != Tok_Comment || toks[1].Type != Tok_n {
		t.Fatalf("got %+v", toks)
	}
	if toks[0].Val != "<< a comment >>" {
		t.Fatalf("comment val = %q", toks[0].Val)
	}
}

func TestUnterminatedBlockCommentDoesNotPanic(t *testing.T) {
	lx := NewLexer()
	lx.SetIgnoreComments(false)
	lx.SetPackComments(false)
	// Must not panic (the original set the column to -1 here).
	_ = lx.Tokens("foo << unterminated")
}

func assertTypes(t *testing.T, got, want []TokenType) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length: got %d %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d: got %d, want %d\nfull got=%v", i, got[i], want[i], got)
		}
	}
}
