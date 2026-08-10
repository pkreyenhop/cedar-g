package mesa

import "fmt"

// TokKind enumerates lexical token categories for Mini-Mesa.
type TokKind int

const (
	TEOF TokKind = iota
	TIdent
	TInt
	TReal
	TChar
	TString
	TKeyword
	TPunct // operators and punctuation
)

// Token is a single lexical unit with source position for diagnostics.
type Token struct {
	Kind TokKind
	Text string // literal spelling / keyword / punct
	Int  int64
	Real float64
	Char rune
	Str  string
	Line int
	Col  int
}

func (t Token) String() string {
	switch t.Kind {
	case TEOF:
		return "<eof>"
	case TInt:
		return fmt.Sprintf("%d", t.Int)
	case TReal:
		return fmt.Sprintf("%g", t.Real)
	case TChar:
		return fmt.Sprintf("'%c", t.Char)
	case TString:
		return fmt.Sprintf("%q", t.Str)
	default:
		return t.Text
	}
}

// keywords are reserved structural/control words. Base type names
// (INTEGER, REAL, ...) and TRUE/FALSE are predefined identifiers, not
// keywords, so they live in the interpreter's global environment.
var keywords = map[string]bool{
	"PROGRAM": true, "DEFINITIONS": true, "MODULE": true, "MONITOR": true,
	"DIRECTORY": true,
	"BEGIN": true, "END": true,
	"IF": true, "THEN": true, "ELSE": true,
	"DO": true, "ENDLOOP": true, "FOR": true, "WHILE": true,
	"UNTIL": true, "THROUGH": true, "IN": true,
	"SELECT": true, "FROM": true, "ENDCASE": true,
	"RETURN": true, "RETURNS": true, "PROCEDURE": true, "PROC": true,
	"TYPE": true, "RECORD": true, "ARRAY": true, "OF": true,
	"EXIT": true, "LOOP": true, "NEW": true, "NIL": true, "NULL": true,
	"AND": true, "OR": true, "NOT": true, "MOD": true,
}
