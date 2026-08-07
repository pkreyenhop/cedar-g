package cedar

import (
	"bytes"
	"image/color"
	"strings"
)

// highlight.go ports CedarHighlighter.{h,cpp}. The Qt version subclassed
// QSyntaxHighlighter and coloured one text block (line) at a time, carrying a
// small state between blocks for multi-line "<< >>" comments. Here Highlight
// runs the same logic across all lines and returns styled spans, keeping the
// port independent of any particular GUI toolkit.

// Category is a highlighting class, matching the original Highlighter::Category.
type Category int

const (
	C_Num Category = iota
	C_Str
	C_Kw
	C_Type
	C_Ident
	C_Op
	C_Pp
	C_Cmt
	C_Label
	C_Sym
	C_Max
)

// Span is a coloured range within the document. Row is a 0-based line index;
// Col is a 0-based byte offset within that line; Len is the span length.
type Span struct {
	Row, Col, Len int
	Cat           Category
}

// Style describes how a category is drawn. Foreground/background are only
// applied when their Has* flag is set, so identifiers keep the theme default.
type Style struct {
	FG     color.RGBA
	HasFG  bool
	BG     color.RGBA
	HasBG  bool
	Bold   bool
	Italic bool
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 0xff} }

// CategoryStyle returns the drawing style for a category, using the same colours
// as the original Qt highlighter.
func CategoryStyle(c Category) Style {
	switch c {
	case C_Num:
		return Style{FG: rgb(0, 153, 153), HasFG: true}
	case C_Str:
		return Style{FG: rgb(208, 16, 64), HasFG: true}
	case C_Sym:
		return Style{FG: rgb(210, 105, 30), HasFG: true}
	case C_Cmt:
		return Style{FG: rgb(153, 153, 136), HasFG: true}
	case C_Kw:
		return Style{FG: rgb(68, 85, 136), HasFG: true, Bold: true}
	case C_Op:
		return Style{FG: rgb(153, 0, 0), HasFG: true, Bold: true}
	case C_Type:
		return Style{FG: rgb(153, 0, 115), HasFG: true, Bold: true}
	case C_Pp:
		return Style{FG: rgb(0, 128, 0), HasFG: true, Bold: true, BG: rgb(230, 255, 230), HasBG: true}
	case C_Label:
		return Style{FG: rgb(251, 138, 0), HasFG: true, BG: rgb(253, 217, 165), HasBG: true}
	default: // C_Ident and anything else: theme default colour.
		return Style{}
	}
}

// CategoryStyleMono returns a colourless style that distinguishes categories by
// weight and slant only (bold/italic), for a monochrome view in the spirit of
// Tioga's text "looks". Colour is never set, so all text stays the theme's.
func CategoryStyleMono(c Category) Style {
	switch c {
	case C_Kw, C_Op, C_Pp, C_Label:
		return Style{Bold: true}
	case C_Type:
		return Style{Bold: true, Italic: true}
	case C_Cmt, C_Str:
		return Style{Italic: true}
	default: // numbers, symbols, identifiers: plain.
		return Style{}
	}
}

// Highlight scans the source text line by line and returns the spans to colour.
// builtins is the set of identifiers to render as types (the viewer's builtin
// list); it may be nil.
func Highlight(text string, builtins map[string]bool) []Span {
	lines := strings.Split(text, "\n")
	var spans []Span

	lexerState := 0 // 1 => currently inside a multi-line "<< >>" comment
	braceDepth := 0
	lx := NewLexer()
	lx.SetIgnoreComments(false)
	lx.SetPackComments(false)

	for row, line := range lines {
		// Work in the Latin-1/rune domain so column numbers equal the rune-cell
		// indices a TextGrid uses (each Latin-1 byte maps to exactly one rune).
		lb := toLatin1(line)
		start := 0
		if lexerState == 1 {
			// Continuation of a multi-line "<< >>" comment.
			pos := bytes.Index(lb, []byte(">>"))
			if pos == -1 {
				if len(lb) > 0 {
					spans = append(spans, Span{Row: row, Col: 0, Len: len(lb), Cat: C_Cmt})
				}
				continue
			}
			pos += 2
			spans = append(spans, Span{Row: row, Col: 0, Len: pos, Cat: C_Cmt})
			lexerState = 0
			braceDepth--
			start = pos
		}

		toks := lx.tokensLatin1(lb[start:])
		for _, t := range toks {
			col := t.ColNr + start // absolute 1-based column
			var cat Category
			has := true
			switch {
			case t.Type == Tok_Comment:
				cat = C_Cmt
			case t.Type == Tok_2Lt:
				cat = C_Cmt
				lexerState = 1
				if strings.HasSuffix(t.Val, ">>") {
					lexerState = 0
				} else {
					braceDepth++
				}
			case t.Type == Tok_string || t.Type == Tok_char:
				cat = C_Str
			case t.Type == Tok_number:
				cat = C_Num
			case IsLiteral(t.Type):
				cat = C_Op
			case IsKeyword(t.Type):
				cat = C_Kw
			case t.Type == Tok_n:
				if builtins[t.Val] {
					cat = C_Type
				} else {
					cat = C_Ident
				}
			case t.Type == Tok_symbol:
				cat = C_Sym
			default:
				has = false
			}
			if !has {
				continue
			}
			n := t.Len
			if t.Val != "" {
				n = len(t.Val)
			}
			spans = append(spans, Span{Row: row, Col: col - 1, Len: n, Cat: cat})
		}
	}
	return spans
}
