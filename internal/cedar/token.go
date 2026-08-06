package cedar

// Token is a single lexed token. It ports the essential fields of the original
// Cedar::Token; the parser-only interned id field is omitted since the parser
// is not part of this port.
type Token struct {
	Type       TokenType
	Len        int    // length of the token in source bytes
	LineNr     int    // 1-based line number
	ColNr      int    // 1-based column number
	Val        string // raw source text of the token (when meaningful)
	SourcePath string
}

// IsValid reports whether the token is neither EOF nor invalid.
func (t Token) IsValid() bool { return t.Type != Tok_Eof && t.Type != Tok_Invalid }
