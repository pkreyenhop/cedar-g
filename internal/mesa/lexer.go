package mesa

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Lexer turns Mesa source into a flat slice of tokens. Tokenising up
// front makes the parser's occasional backtracking (declaration vs.
// statement) trivial.
type Lexer struct {
	src  string
	pos  int
	line int
	col  int
}

func NewLexer(src string) *Lexer {
	return &Lexer{src: src, line: 1, col: 1}
}

func (l *Lexer) errorf(format string, a ...any) error {
	return fmt.Errorf("lex error at line %d:%d: %s", l.line, l.col, fmt.Sprintf(format, a...))
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
	return r
}

func (l *Lexer) peek2() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	_, w := utf8.DecodeRuneInString(l.src[l.pos:])
	if l.pos+w >= len(l.src) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.src[l.pos+w:])
	return r
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	r, w := utf8.DecodeRuneInString(l.src[l.pos:])
	l.pos += w
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

// Tokenize returns all tokens ending with a TEOF sentinel.
func (l *Lexer) Tokenize() ([]Token, error) {
	var toks []Token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.Kind == TEOF {
			return toks, nil
		}
	}
}

func (l *Lexer) skipSpaceAndComments() error {
	for {
		r := l.peek()
		switch {
		case r == 0:
			return nil
		case unicode.IsSpace(r):
			l.advance()
		case r == '<' && l.peek2() == '<':
			// Cedar block comment: << ... >>, nestable.
			depth := 0
			for {
				c := l.peek()
				if c == 0 {
					break
				}
				if c == '<' && l.peek2() == '<' {
					l.advance()
					l.advance()
					depth++
					continue
				}
				if c == '>' && l.peek2() == '>' {
					l.advance()
					l.advance()
					depth--
					if depth == 0 {
						break
					}
					continue
				}
				l.advance()
			}
		case r == '-' && l.peek2() == '-':
			// Mesa comment: -- ... terminated by another -- or end of line.
			l.advance()
			l.advance()
			for {
				c := l.peek()
				if c == 0 || c == '\n' {
					break
				}
				if c == '-' && l.peek2() == '-' {
					l.advance()
					l.advance()
					break
				}
				l.advance()
			}
		default:
			return nil
		}
	}
}

func (l *Lexer) next() (Token, error) {
	if err := l.skipSpaceAndComments(); err != nil {
		return Token{}, err
	}
	line, col := l.line, l.col
	r := l.peek()
	if r == 0 {
		return Token{Kind: TEOF, Line: line, Col: col}, nil
	}

	// Identifiers / keywords
	if isIdentStart(r) {
		var sb strings.Builder
		for isIdentPart(l.peek()) {
			sb.WriteRune(l.advance())
		}
		text := sb.String()
		if keywords[text] {
			return Token{Kind: TKeyword, Text: text, Line: line, Col: col}, nil
		}
		return Token{Kind: TIdent, Text: text, Line: line, Col: col}, nil
	}

	// Numbers
	if unicode.IsDigit(r) {
		return l.lexNumber(line, col)
	}

	// Character literal: 'c  (Mesa style, no closing quote)
	if r == '\'' {
		l.advance()
		c := l.advance()
		if c == 0 {
			return Token{}, l.errorf("unterminated character literal")
		}
		return Token{Kind: TChar, Char: c, Text: "'" + string(c), Line: line, Col: col}, nil
	}

	// String literal
	if r == '"' {
		return l.lexString(line, col)
	}

	return l.lexPunct(line, col)
}

func (l *Lexer) lexNumber(line, col int) (Token, error) {
	start := l.pos
	for unicode.IsDigit(l.peek()) {
		l.advance()
	}

	// Real number: a fractional part or an exponent.
	isReal := false
	if l.peek() == '.' && l.peek2() != '.' && unicode.IsDigit(l.peek2()) {
		isReal = true
		l.advance()
		for unicode.IsDigit(l.peek()) {
			l.advance()
		}
	}
	if l.peek() == 'e' || l.peek() == 'E' {
		isReal = true
		l.advance()
		if l.peek() == '+' || l.peek() == '-' {
			l.advance()
		}
		for unicode.IsDigit(l.peek()) {
			l.advance()
		}
	}
	if isReal {
		text := l.src[start:l.pos]
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return Token{}, l.errorf("bad real literal %q", text)
		}
		return Token{Kind: TReal, Real: f, Text: text, Line: line, Col: col}, nil
	}

	// Integer, possibly with a Mesa base/char suffix. Hex digits may follow the
	// decimal run; the suffix letter (H hex, B octal, C octal char code, D
	// decimal) is not itself a hex digit except when it is 'H' (which is not).
	for isHexDigit(l.peek()) {
		l.advance()
	}
	body := l.src[start:l.pos]
	base := 10
	isChar := false
	if c := l.peek(); c == 'H' || c == 'h' {
		l.advance()
		base = 16
	} else if n := len(body); n > 0 {
		switch body[n-1] {
		case 'B', 'b':
			body, base = body[:n-1], 8
		case 'C', 'c':
			body, base, isChar = body[:n-1], 8, true // octal character code
		case 'D', 'd':
			body, base = body[:n-1], 10
		}
	}
	text := l.src[start:l.pos]
	n, err := strconv.ParseInt(body, base, 64)
	if err != nil {
		return Token{}, l.errorf("bad integer literal %q", text)
	}
	if isChar {
		return Token{Kind: TChar, Char: rune(n), Text: text, Line: line, Col: col}, nil
	}
	return Token{Kind: TInt, Int: n, Text: text, Line: line, Col: col}, nil
}

func isHexDigit(r rune) bool {
	return unicode.IsDigit(r) ||
		(r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func (l *Lexer) lexString(line, col int) (Token, error) {
	l.advance() // opening "
	var sb strings.Builder
	for {
		c := l.peek()
		if c == 0 {
			return Token{}, l.errorf("unterminated string literal")
		}
		if c == '"' {
			l.advance()
			break
		}
		if c == '\\' {
			l.advance()
			e := l.advance()
			switch e {
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case 'r':
				sb.WriteRune('\r')
			case '"':
				sb.WriteRune('"')
			case '\\':
				sb.WriteRune('\\')
			default:
				sb.WriteRune(e)
			}
			continue
		}
		sb.WriteRune(l.advance())
	}
	return Token{Kind: TString, Str: sb.String(), Text: sb.String(), Line: line, Col: col}, nil
}

// multi-character punctuation, longest match first
var multiPunct = []string{"<=", ">=", "<-", "..", "=>", "**", "~=", "#="}

func (l *Lexer) lexPunct(line, col int) (Token, error) {
	// Mesa left-arrow assignment (unicode ←) and up-arrow dereference. The Tioga
	// decoder yields U+00AD for the Xerox up-arrow, which Cedar uses like '^'.
	if l.peek() == '←' {
		l.advance()
		return Token{Kind: TPunct, Text: "<-", Line: line, Col: col}, nil
	}
	if l.peek() == '↑' || l.peek() == '­' {
		l.advance()
		return Token{Kind: TPunct, Text: "^", Line: line, Col: col}, nil
	}
	// Multi-char ASCII operators
	rest := l.src[l.pos:]
	for _, p := range multiPunct {
		if strings.HasPrefix(rest, p) {
			for range p {
				l.advance()
			}
			return Token{Kind: TPunct, Text: p, Line: line, Col: col}, nil
		}
	}
	r := l.advance()
	switch r {
	case '+', '-', '*', '/', '=', '#', '<', '>',
		'(', ')', '[', ']', '{', '}', ',', ';', ':', '.', '@', '^',
		'~', '!', '&', '|':
		return Token{Kind: TPunct, Text: string(r), Line: line, Col: col}, nil
	case '$':
		// Cedar atom literal: $Name. The name is optional in odd positions.
		if isIdentStart(l.peek()) {
			var sb strings.Builder
			for isIdentPart(l.peek()) {
				sb.WriteRune(l.advance())
			}
			return Token{Kind: TString, Str: sb.String(), Text: "$" + sb.String(), Line: line, Col: col}, nil
		}
		return Token{Kind: TPunct, Text: "$", Line: line, Col: col}, nil
	}
	return Token{}, l.errorf("unexpected character %q", string(r))
}

func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIdentPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
