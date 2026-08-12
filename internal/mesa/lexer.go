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
	src       string
	pos       int
	line      int
	col       int
	lineStart bool // no code token yet on the current line
}

func NewLexer(src string) *Lexer {
	return &Lexer{src: src, line: 1, col: 1, lineStart: true}
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
		l.lineStart = true
	} else {
		l.col++
	}
	return r
}

// Tokenize returns all tokens ending with a TEOF sentinel. It stops at the
// module terminator "END." — the keyword END immediately followed by '.' — since
// everything after it is trailing free text (change logs, mail headers) the
// compiler ignores. Lexing that text as code would otherwise fail on stray
// punctuation like '?' or control bytes.
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
		if t.Kind == TKeyword && t.Text == "END" {
			save := *l
			nt, nerr := l.next()
			if nerr == nil && nt.Kind == TPunct && nt.Text == "." {
				toks = append(toks, nt, Token{Kind: TEOF, Line: nt.Line, Col: nt.Col})
				return toks, nil
			}
			*l = save // not a module terminator: rewind and keep lexing
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
			// A '--' comment. After code it is an inline comment bounded by the next
			// '--'. At the start of a line it normally runs to end of line, so a prose
			// comment whose text happens to contain '--' stays a comment. The one
			// exception is a tightly-wrapped commented-out fragment such as
			// "--BackStop,-- BasicTime, …;": a line-start '--' with no following space
			// is a bounded comment, so the real code after it (and a trailing ';' that
			// ends a DIRECTORY clause) stays visible.
			l.advance()
			l.advance()
			bounded := !l.lineStart || (l.peek() != ' ' && l.peek() != '\t')
			for {
				c := l.peek()
				if c == 0 || c == '\n' {
					break
				}
				if bounded && c == '-' && l.peek2() == '-' {
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
	l.lineStart = false // a real token now sits on this line

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

	// Numbers, including a leading-dot real such as ".5" (but not "..").
	if unicode.IsDigit(r) || (r == '.' && unicode.IsDigit(l.peek2())) {
		return l.lexNumber(line, col)
	}

	// Character literal: 'c  (Mesa style, no closing quote); supports escapes
	// like '\n, '\\ and octal '\177.
	if r == '\'' {
		l.advance()
		c := l.advance()
		if c == 0 {
			return Token{}, l.errorf("unterminated character literal")
		}
		if c == '\\' {
			c = l.charEscape()
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

	// Hex-literal lookahead: <hexdigits>H is hex, so the 'E' in 36E9H is a hex
	// digit, not an exponent. Scan ahead and, if an H terminates the run, take it.
	j := l.pos
	for j < len(l.src) && isHexByte(l.src[j]) {
		j++
	}
	if j < len(l.src) && (l.src[j] == 'H' || l.src[j] == 'h') {
		for l.pos < j {
			l.advance()
		}
		body := l.src[start:l.pos]
		l.advance() // consume 'H'
		n, err := strconv.ParseInt(body, 16, 64)
		if err != nil {
			return Token{}, l.errorf("bad hex literal %q", l.src[start:l.pos])
		}
		return Token{Kind: TInt, Int: n, Text: l.src[start:l.pos], Line: line, Col: col}, nil
	}

	// Real number: a fractional part (possibly a bare trailing dot, "1.") or an
	// exponent. A ".." is the range operator, not a fraction.
	isReal := false
	if l.peek() == '.' && l.peek2() != '.' {
		isReal = true
		l.advance()
		for unicode.IsDigit(l.peek()) {
			l.advance()
		}
	}
	// An exponent — 'E' as usual, or Cedar's 'D' scaling (1D3 == 1×10³) — but
	// only when a digit or sign follows, so a hex literal like 0EH is not one.
	scaled := false
	if (l.peek() == 'e' || l.peek() == 'E' || l.peek() == 'D' || l.peek() == 'd') &&
		(unicode.IsDigit(l.peek2()) || l.peek2() == '+' || l.peek2() == '-') {
		scaled = l.peek() == 'D' || l.peek() == 'd'
		isReal = true
		expStart := l.pos
		l.advance()
		if l.peek() == '+' || l.peek() == '-' {
			l.advance()
		}
		for unicode.IsDigit(l.peek()) {
			l.advance()
		}
		if scaled { // rewrite 'D' to 'e' so ParseFloat accepts it
			s := l.src[start:expStart] + "e" + l.src[expStart+1:l.pos]
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return Token{}, l.errorf("bad scaled literal %q", l.src[start:l.pos])
			}
			return Token{Kind: TReal, Real: f, Text: l.src[start:l.pos], Line: line, Col: col}, nil
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

	// Integer with an optional Mesa base marker. Gather the maximal run of hex
	// digits (a letter A–F may be a hex digit, an octal 'B' marker, or a 'C'/'D'
	// suffix — resolved below), then classify:
	//   • trailing H/h                       → hex             (0FFH, 36E9H)
	//   • <octal digits> B [scale digits]    → octal × 8^scale (377B, 144B2)
	//   • trailing C/c                        → octal char code
	//   • trailing D/d                        → explicit decimal
	//   • leading 0 with a hex letter, no marker → hex          (0FF, 0c3d2e1f0)
	//   • otherwise                           → decimal
	for isHexDigit(l.peek()) {
		l.advance()
	}
	body := l.src[start:l.pos]
	base := 10
	isChar := false
	switch {
	case l.peek() == 'H' || l.peek() == 'h':
		l.advance()
		base = 16
	case len(body) > 0 && octalMarker(body):
		val, err := octalValue(body)
		if err != nil {
			return Token{}, l.errorf("bad octal literal %q", body)
		}
		return Token{Kind: TInt, Int: val, Text: body, Line: line, Col: col}, nil
	case len(body) > 0 && (body[len(body)-1] == 'C' || body[len(body)-1] == 'c'):
		body, base, isChar = body[:len(body)-1], 8, true // octal character code
	case len(body) > 0 && (body[len(body)-1] == 'D' || body[len(body)-1] == 'd'):
		body, base = body[:len(body)-1], 10
	case len(body) > 1 && body[0] == '0' && hasHexLetter(body):
		base = 16 // Cedar hex written with a leading 0 and no H, e.g. 0FF
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

// octalMarker reports whether body has the Mesa octal shape
// "<octal digits> B [decimal scale]" — octal digits (0–7), the marker B, then an
// optional decimal scale. It is checked before the suffixless-hex rule so 144B2
// is octal-with-scale, not a malformed hex value.
func octalMarker(body string) bool {
	i := 0
	for i < len(body) && body[i] >= '0' && body[i] <= '7' {
		i++
	}
	if i == 0 || i >= len(body) || (body[i] != 'B' && body[i] != 'b') {
		return false
	}
	for _, c := range body[i+1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// octalValue evaluates an octalMarker body: the octal part times 8^scale.
func octalValue(body string) (int64, error) {
	i := 0
	for body[i] >= '0' && body[i] <= '7' {
		i++
	}
	v, err := strconv.ParseInt(body[:i], 8, 64)
	if err != nil {
		return 0, err
	}
	if rest := body[i+1:]; rest != "" {
		scale, err := strconv.Atoi(rest)
		if err != nil {
			return 0, err
		}
		for k := 0; k < scale; k++ {
			v *= 8
		}
	}
	return v, nil
}

// hasHexLetter reports whether s contains a hexadecimal letter (a–f/A–F),
// which marks an unsuffixed value as hexadecimal.
func hasHexLetter(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			return true
		}
	}
	return false
}

// charEscape decodes the character after a backslash in a char literal.
func (l *Lexer) charEscape() rune {
	e := l.advance()
	switch e {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case 'b':
		return '\b'
	case 'f':
		return '\f'
	case 'l':
		return '\n'
	case '\\', '\'', '"':
		return e
	}
	if e >= '0' && e <= '7' { // octal escape \ddd
		v := int(e - '0')
		for i := 0; i < 2 && l.peek() >= '0' && l.peek() <= '7'; i++ {
			v = v*8 + int(l.advance()-'0')
		}
		return rune(v)
	}
	return e
}

func isHexDigit(r rune) bool {
	return unicode.IsDigit(r) ||
		(r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func isHexByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func (l *Lexer) lexString(line, col int) (Token, error) {
	l.advance() // opening "
	var sb strings.Builder
	for {
		if l.pos >= len(l.src) { // true end of input, not a NUL content byte
			return Token{}, l.errorf("unterminated string literal")
		}
		c := l.peek()
		if c == '"' {
			l.advance()
			if l.peek() == '"' { // "" is an escaped quote inside the string
				sb.WriteRune(l.advance())
				continue
			}
			if l.peek() == 'L' { // a "…"L long-string literal suffix
				l.advance()
			}
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

// multi-character punctuation, longest match first. ":=" is Pascal-style
// assignment, treated the same as Mesa's "←".
var multiPunct = []string{":=", "<=", ">=", "<-", "..", "=>", "**", "~=", "#="}

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
			text := p
			if text == ":=" { // Pascal-style assignment == "←"
				text = "<-"
			}
			return Token{Kind: TPunct, Text: text, Line: line, Col: col}, nil
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
