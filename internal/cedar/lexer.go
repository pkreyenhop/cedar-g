package cedar

// lexer.go ports CedarLexer.{h,cpp}. Like the original it works on a Latin-1
// byte view of the source: the assignment arrow "←" (U+2190) is mapped to the
// single byte 0xAC ('¬' in Latin-1), which the Cedar sources use for the same
// purpose.

// negSym is the Latin-1 byte the lexer treats as the "←" assignment token.
const negSym = 0xac

// Lexer is a Cedar/Mesa scanner. The zero value is not usable; call NewLexer.
type Lexer struct {
	buf    []byte
	bufPos int

	line   []byte
	lineNr int // 1-based
	colNr  int // 0-based index into line

	buffer     []Token // lookahead buffer
	lastToken  Token
	sloc       int // lines of code excluding empty/comment-only lines
	filePath   string
	ignoreCmts bool
	packCmts   bool
	lineCnted  bool
}

// NewLexer returns a lexer with the same defaults as the original
// (comments ignored and packed).
func NewLexer() *Lexer {
	return &Lexer{ignoreCmts: true, packCmts: true}
}

// SetIgnoreComments controls whether comment tokens are delivered.
func (l *Lexer) SetIgnoreComments(b bool) { l.ignoreCmts = b }

// SetPackComments controls whether a "<< >>" comment is delivered as a single
// Tok_Comment (true) or as Tok_2Lt (false, used by the highlighter).
func (l *Lexer) SetPackComments(b bool) { l.packCmts = b }

// Sloc returns the number of source lines seen that contained code.
func (l *Lexer) Sloc() int { return l.sloc }

// SetStream loads code for scanning. The "←" runes are mapped to negSym and the
// text is reduced to Latin-1 bytes, matching the original QBuffer setup.
func (l *Lexer) SetStream(code, filePath string) {
	l.buf = toLatin1(code)
	l.bufPos = 0
	l.line = nil
	l.lineNr = 0
	l.colNr = 0
	l.buffer = nil
	l.lastToken = Token{}
	l.filePath = filePath
	l.sloc = 0
	l.lineCnted = false
}

// setStreamLatin1 loads an already-Latin-1 byte buffer directly, skipping the
// UTF-8→Latin-1 conversion. Used by the highlighter, which needs column numbers
// in the Latin-1/rune domain.
func (l *Lexer) setStreamLatin1(buf []byte, filePath string) {
	l.buf = buf
	l.bufPos = 0
	l.line = nil
	l.lineNr = 0
	l.colNr = 0
	l.buffer = nil
	l.lastToken = Token{}
	l.filePath = filePath
	l.sloc = 0
	l.lineCnted = false
}

// tokensLatin1 scans an already-Latin-1 buffer and returns all valid tokens.
func (l *Lexer) tokensLatin1(buf []byte) []Token {
	l.setStreamLatin1(buf, "")
	var res []Token
	t := l.NextToken()
	for t.IsValid() {
		res = append(res, t)
		t = l.NextToken()
	}
	return res
}

// toLatin1 mirrors QString::toLatin1 after replacing "←" with negSym: each rune
// becomes a single byte; runes outside Latin-1 become '?'.
func toLatin1(code string) []byte {
	out := make([]byte, 0, len(code))
	for _, r := range code {
		switch {
		case r == '←':
			out = append(out, negSym)
		case r <= 0xff:
			out = append(out, byte(r))
		default:
			out = append(out, '?')
		}
	}
	return out
}

// NextToken returns the next token, skipping comments when ignoreComments is set.
func (l *Lexer) NextToken() Token {
	var t Token
	if len(l.buffer) > 0 {
		t = l.buffer[0]
		l.buffer = l.buffer[1:]
	} else {
		t = l.nextTokenImp()
	}
	for t.Type == Tok_Comment && l.ignoreCmts {
		t = l.NextToken()
	}
	return t
}

// PeekToken returns the token lookAhead positions ahead (1 = next) without
// consuming it.
func (l *Lexer) PeekToken(lookAhead int) Token {
	if lookAhead < 1 {
		lookAhead = 1
	}
	for len(l.buffer) < lookAhead {
		t := l.nextTokenImp()
		for t.Type == Tok_Comment && l.ignoreCmts {
			t = l.nextTokenImp()
		}
		l.buffer = append(l.buffer, t)
	}
	return l.buffer[lookAhead-1]
}

// Tokens scans the whole of code and returns all valid tokens.
func (l *Lexer) Tokens(code string) []Token {
	l.SetStream(code, "")
	var res []Token
	t := l.NextToken()
	for t.IsValid() {
		res = append(res, t)
		t = l.NextToken()
	}
	return res
}

func (l *Lexer) atEnd() bool { return l.bufPos >= len(l.buf) }

func (l *Lexer) nextTokenImp() Token {
	if l.buf == nil {
		return l.token(Tok_Eof, 0, "")
	}
	l.skipWhiteSpace()

	for l.colNr >= len(l.line) {
		if l.atEnd() {
			return l.token(Tok_Eof, 0, "")
		}
		l.nextLine()
		l.skipWhiteSpace()
	}

	for l.colNr < len(l.line) {
		ch := l.line[l.colNr]
		switch {
		case ch == '"':
			return l.string()
		case ch == negSym || ch == '_':
			return l.token(Tok_2190, 1, "_")
		case ch == '\'':
			return l.character()
		case ch == '$':
			return l.symbol()
		case isAlpha(ch):
			return l.ident()
		case isDigit(ch):
			return l.number()
		}

		tt, pos := matchOperator(l.line, l.colNr)
		if tt == Tok_2Lt {
			return l.comment()
		} else if tt == Tok_2Minus {
			n := len(l.line) - l.colNr
			return l.token(Tok_Comment, n, string(l.line[l.colNr:l.colNr+n]))
		} else if tt == Tok_Invalid || pos == l.colNr {
			return l.token(Tok_Invalid, 1, "unexpected character")
		}
		n := pos - l.colNr
		return l.token(tt, n, string(l.line[l.colNr:l.colNr+n]))
	}
	return l.token(Tok_Invalid, 1, "")
}

func (l *Lexer) skipWhiteSpace() int {
	start := l.colNr
	for l.colNr < len(l.line) && isSpace(l.line[l.colNr]) {
		l.colNr++
	}
	return l.colNr - start
}

func (l *Lexer) nextLine() {
	l.colNr = 0
	l.lineNr++
	start := l.bufPos
	for l.bufPos < len(l.buf) && l.buf[l.bufPos] != '\n' {
		l.bufPos++
	}
	if l.bufPos < len(l.buf) { // consume the '\n'
		l.bufPos++
	}
	line := l.buf[start:l.bufPos]
	// Chop the trailing newline, matching QByteArray::chop after readLine.
	if n := len(line); n >= 2 && line[n-2] == '\r' && line[n-1] == '\n' {
		line = line[:n-2]
	} else if n := len(line); n >= 1 && (line[n-1] == '\n' || line[n-1] == '\r') {
		line = line[:n-1]
	}
	l.line = line
	l.lineCnted = false
}

// lookAhead returns the byte off positions past the current column, or 0.
func (l *Lexer) lookAhead(off int) byte {
	if l.colNr+off < len(l.line) && l.colNr+off >= 0 {
		return l.line[l.colNr+off]
	}
	return 0
}

// token builds a token of the given length and advances the column.
func (l *Lexer) token(tt TokenType, n int, val string) Token {
	if tt != Tok_Invalid && tt != Tok_Comment && tt != Tok_Eof {
		l.countLine()
	}
	t := Token{Type: tt, LineNr: l.lineNr, ColNr: l.colNr + 1, Val: val, SourcePath: l.filePath}
	l.lastToken = t
	l.colNr += n
	t.Len = n
	return t
}

func (l *Lexer) ident() Token {
	off := 1
	for isAlnum(l.lookAhead(off)) {
		off++
	}
	str := string(l.line[l.colNr : l.colNr+off])
	// Only a full-length keyword match counts; otherwise it's an identifier.
	if t := keywordType(str); t != Tok_Invalid {
		return l.token(t, off, "")
	}
	return l.token(Tok_n, off, str)
}

func (l *Lexer) number() Token {
	off := 1
	for isDigit(l.lookAhead(off)) {
		off++
	}
	if l.lookAhead(off) == '.' && l.lookAhead(off+1) != '.' {
		off++
		if !isDigit(l.lookAhead(off)) {
			return l.token(Tok_Invalid, off, "invalid real, digit expected after dot")
		}
		for isDigit(l.lookAhead(off)) {
			off++
		}
	}
	if l.lookAhead(off) == 'E' || l.lookAhead(off) == 'e' {
		off++
		o := l.lookAhead(off)
		if o == '+' || o == '-' {
			off++
			o = l.lookAhead(off)
		}
		if !isDigit(o) {
			return l.token(Tok_Invalid, off, "invalid real, digit expected after exponent")
		}
		for isDigit(l.lookAhead(off)) {
			off++
		}
	}
	str := string(l.line[l.colNr : l.colNr+off])
	return l.token(Tok_number, off, str)
}

func (l *Lexer) symbol() Token {
	off := 1
	for isAlnum(l.lookAhead(off)) {
		off++
	}
	str := string(l.line[l.colNr : l.colNr+off])
	return l.token(Tok_symbol, off, str)
}

// comment scans a "<< ... >>" comment, which may span multiple lines.
func (l *Lexer) comment() Token {
	startLine := l.lineNr
	startCol := l.colNr

	const tag = ">>"
	pos := indexByte2(l.line, l.colNr, tag)

	var str []byte
	terminated := false
	if pos < 0 {
		str = append([]byte(nil), l.line[l.colNr:]...)
	} else {
		terminated = true
		pos += len(tag)
		str = append([]byte(nil), l.line[l.colNr:pos]...)
	}
	for !terminated && !l.atEnd() {
		l.nextLine()
		pos = indexByte2(l.line, l.colNr, tag)
		if len(str) > 0 {
			str = append(str, '\n')
		}
		if pos < 0 {
			str = append(str, l.line[l.colNr:]...)
		} else {
			terminated = true
			pos += len(tag)
			str = append(str, l.line[l.colNr:pos]...)
		}
	}
	if l.packCmts && !terminated && l.atEnd() {
		l.colNr = len(l.line)
		return Token{Type: Tok_Invalid, LineNr: startLine, ColNr: startCol + 1,
			Val: "non-terminated comment", SourcePath: l.filePath}
	}
	tt := Tok_2Lt
	if l.packCmts {
		tt = Tok_Comment
	}
	t := Token{Type: tt, LineNr: startLine, ColNr: startCol + 1, Val: string(str), SourcePath: l.filePath}
	l.lastToken = t
	// The original set colNr to pos here, which is -1 when the comment is
	// unterminated (no ">>"), leading to an out-of-bounds read on the next
	// scan. Consume to end of line instead, which is the intended effect.
	if pos < 0 {
		l.colNr = len(l.line)
	} else {
		l.colNr = pos
	}
	return t
}

func (l *Lexer) character() Token {
	if l.lookAhead(1) == '\\' {
		ch := l.lookAhead(2)
		switch ch {
		case 'n', 'N', 'r', 'R', 't', 'T', 'b', 'B', 'f', 'F', 'l', 'L', '\'', '"', '\\':
			return l.token(Tok_char, 3, string(l.line[l.colNr:l.colNr+3]))
		default:
			if isDigit(ch) && isDigit(l.lookAhead(3)) && isDigit(l.lookAhead(4)) {
				return l.token(Tok_char, 5, string(l.line[l.colNr:l.colNr+5]))
			}
			return l.token(Tok_Invalid, l.colNr, "invalid character escape code")
		}
	}
	return l.token(Tok_char, 2, string(l.line[l.colNr:l.colNr+2]))
}

func (l *Lexer) string() Token {
	// Note: like the original, strings are not yet allowed to span lines.
	off := 1
	for {
		c := l.lookAhead(off)
		off++
		if c == '\\' {
			off++
		} else if c == '"' {
			break
		}
		if c == 0 {
			return l.token(Tok_Invalid, off, "non-terminated string")
		}
	}
	end := l.colNr + off
	if end > len(l.line) {
		end = len(l.line)
	}
	str := string(l.line[l.colNr:end])
	return l.token(Tok_string, off, str)
}

func (l *Lexer) countLine() {
	if !l.lineCnted {
		l.sloc++
	}
	l.lineCnted = true
}

// indexByte2 returns the index of the two-byte tag in s at or after start, or -1.
func indexByte2(s []byte, start int, tag string) int {
	for i := start; i+1 < len(s); i++ {
		if s[i] == tag[0] && s[i+1] == tag[1] {
			return i
		}
	}
	return -1
}

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlnum(c byte) bool { return isAlpha(c) || isDigit(c) }
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}
