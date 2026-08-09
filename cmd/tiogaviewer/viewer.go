package main

import (
	"image"
	"os"
	"sort"
	"strings"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"cedarg/internal/cedar"
	"cedarg/internal/tioga"
)

// builtinList are identifiers rendered as types (the original viewer's list).
var builtinList = []string{
	"ATOM", "BOOL", "BOOLEAN", "CARDINAL", "CHAR", "CHARACTER", "CODE",
	"ELSE", "ISTYPE", "PACKED", "SIGNAL", "ENABLE", "JOIN", "PAINTED",
	"SIZE", "END", "LAST", "POINTER", "START", "ENDCASE", "LENGTH", "PORT",
	"STATE", "ENDLOOP", "LIST", "PRED", "STOP", "ENTRY", "LOCKS", "PRIVATE",
	"STRING", "ERROR", "LONG", "PROC", "SUCC", "EXIT", "LOOP", "PROCEDURE",
	"TEXT", "EXITS", "LOOPHOLE", "PROCESS", "THEN", "EXPORTS", "MACHINE",
	"PROGRAM", "THROUGH", "FINISHED", "MAX", "PUBLIC", "TO", "FIRST", "MIN",
	"READONLY", "TRANSFER", "FOR", "MOD", "RECORD", "TRASH", "FORK",
	"MONITOR", "REF", "TRUSTED", "FRAME", "MONITORED", "REJECT", "TYPE",
	"FREE", "NARROW", "RELATIVE", "UNCHECKED", "FROM", "NEW", "REPEAT",
	"UNCOUNTED", "GO", "NIL", "RESTART", "UNTIL", "GOTO", "NOT", "RESUME",
	"USING", "IF", "NOTIFY", "RETRY", "WAIT", "IMPORTS", "NULL", "RETURN",
	"WHILE", "IN", "OF", "RETURNS", "WITH", "INLINE", "OPEN", "SAFE",
	"ZONE", "INT", "OR", "SELECT", "INTEGER", "ORDERED", "SEQUENCE",
	"INTERNAL", "OVERLAID", "SHARES", "TRUE", "FALSE", "CARD",
}

// codeTextSize is the default code font size (larger than the doc body for
// readability; scaled further by zoom).
const codeTextSize = 17

// run is a styled slice of a code line.
type run struct {
	text string
	cat  cedar.Category
}

// viewer is one Cedar "Viewer": a document/code pane living in a column.
type viewer struct {
	path   string
	rel    string
	col    int
	isCode bool
	lines  [][]run       // code
	blocks []tioga.Block // documents
	sc     scroller

	bDestroy, bGrow, bIcon, bSwitch, bSplit widget.Clickable
	bRestore                                widget.Clickable // in the icon tray
	headerHovered                           bool
}

func (s *gioUI) newViewer(path string, col int) *viewer {
	v := &viewer{path: path, rel: s.relPath(path), col: col}
	data, err := os.ReadFile(path)
	switch {
	case err != nil:
		v.blocks = []tioga.Block{{Kind: tioga.Paragraph, Text: "cannot open: " + v.rel}}
	case strings.HasSuffix(path, ".mesa") || strings.Contains(path, ".mesa!"):
		v.isCode = true
		doc := tioga.Read(data, true)
		v.lines = codeToRuns(expandTabs(doc.Code), s.builtins)
	default:
		doc := tioga.Read(data, false)
		for i := range doc.Blocks {
			doc.Blocks[i].Text = expandTabs(doc.Blocks[i].Text)
		}
		v.blocks = doc.Blocks
	}
	return v
}

// layoutViewer draws one bordered Viewer: header (actions + title), rule, body.
func (s *gioUI) layoutViewer(gtx C, v *viewer) D {
	return bordered(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return s.header(gtx, v) }),
			layout.Rigid(hrule),
			layout.Flexed(1, func(gtx C) D { return s.body(gtx, v) }),
		)
	})
}

func (s *gioUI) header(gtx C, v *viewer) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	// Header hover state (from the pass-through zone registered last frame).
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.headerHovered, Kinds: pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			switch pe.Kind {
			case pointer.Enter:
				v.headerHovered = true
			case pointer.Leave:
				v.headerHovered = false
			}
		}
	}

	// The command menu (action buttons + title) is always laid out, so the header
	// height is the same whether the black title bar or the menu is shown.
	dims := layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.UniformInset(2).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bDestroy, "Destroy") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bGrow, "Grow") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bIcon, "Icon") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bSwitch, "Switch") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bSplit, "Split") }),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(6), 0)} }),
					layout.Flexed(1, func(gtx C) D {
						return s.label(gtx, serifFont, font.Bold, font.Regular, 13, v.rel, cedarBlack, 1)
					}),
				)
			})
		}),
	)

	// When not hovered, cover the menu with a black title bar showing the path.
	if !v.headerHovered {
		fillAt(gtx, cedarBlack, image.Rectangle{Max: dims.Size})
		cgtx := gtx
		cgtx.Constraints.Min = dims.Size
		cgtx.Constraints.Max = dims.Size
		layout.W.Layout(cgtx, func(gtx C) D {
			return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D {
				return s.label(gtx, serifFont, font.Bold, font.Regular, 13, v.rel, cedarWhite, 1)
			})
		})
	}

	// Header-wide hover zone on top (pass-through) so hovering reveals the menu
	// without stealing button clicks.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	hst := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.headerHovered)
	hst.Pop()
	pass.Pop()

	return dims
}

func (s *gioUI) body(gtx C, v *viewer) D {
	// The left margin comes from the scroll gutter; add top/right/bottom only.
	if v.isCode {
		return layout.Inset{Top: 4, Right: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return s.scrollList(gtx, &v.sc, len(v.lines), func(gtx C, i int) D {
				return s.codeLine(gtx, v.lines[i])
			})
		})
	}
	return layout.Inset{Top: 4, Right: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = gtx.Constraints.Max
		return s.scrollList(gtx, &v.sc, len(v.blocks), func(gtx C, i int) D {
			return s.docBlock(gtx, v.blocks[i])
		})
	})
}

func (s *gioUI) codeLine(gtx C, runs []run) D {
	children := make([]layout.FlexChild, len(runs))
	for i := range runs {
		r := runs[i]
		bold, ital := styleFor(r.cat)
		weight, style := font.Normal, font.Regular
		if bold {
			weight = font.Bold
		}
		if ital {
			style = font.Italic
		}
		children[i] = layout.Rigid(func(gtx C) D {
			return s.label(gtx, monoFont, weight, style, codeTextSize, r.text, cedarBlack, 1)
		})
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
}

// docTextSize / docLineHeight tune document readability (roomier than the old
// cramped defaults); both scale further with zoom.
const (
	docTextSize   = 18
	docLineHeight = 1.3
)

func (s *gioUI) docBlock(gtx C, b tioga.Block) D {
	fnt, weight, style, size := serifFont, font.Normal, font.Regular, float32(docTextSize)
	top, bottom := unit.Dp(3), unit.Dp(3)
	switch b.Kind {
	case tioga.Heading:
		weight = font.Bold
		if b.Level <= 1 {
			size = 28
		} else {
			size = 22
		}
		top, bottom = unit.Dp(10), unit.Dp(6)
	case tioga.Quote:
		style = font.Italic
	case tioga.Code:
		fnt, size = monoFont, docTextSize
	}
	return layout.Inset{Top: top, Bottom: bottom}.Layout(gtx, func(gtx C) D {
		return s.labelLH(gtx, fnt, weight, style, size, b.Text, cedarBlack, 0, docLineHeight)
	})
}

// expandTabs replaces tab characters with spaces to the next 8-column stop
// (per line). Gio's text renderer draws a raw tab as a missing-glyph box, so
// Tioga's tab-aligned blocks (e.g. address lists) would otherwise show boxes.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	const tw = 8
	var b strings.Builder
	b.Grow(len(s))
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			n := tw - col%tw
			for i := 0; i < n; i++ {
				b.WriteByte(' ')
			}
			col += n
		case '\n':
			b.WriteByte('\n')
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// styleFor returns emphasis for a category: black text, distinguished by
// bold/italic only (Tioga's monochrome "looks").
func styleFor(cat cedar.Category) (bold, italic bool) {
	st := cedar.CategoryStyleMono(cat)
	return st.Bold, st.Italic
}

// codeToRuns turns highlighted code into per-line styled runs (computed once).
func codeToRuns(code string, builtins map[string]bool) [][]run {
	lines := strings.Split(code, "\n")
	byRow := map[int][]cedar.Span{}
	for _, sp := range cedar.Highlight(code, builtins) {
		byRow[sp.Row] = append(byRow[sp.Row], sp)
	}
	out := make([][]run, len(lines))
	for r, line := range lines {
		runes := []rune(line)
		spans := byRow[r]
		sort.Slice(spans, func(i, j int) bool { return spans[i].Col < spans[j].Col })
		var rs []run
		pos := 0
		for _, sp := range spans {
			if sp.Col > len(runes) {
				continue
			}
			if sp.Col > pos {
				rs = append(rs, run{string(runes[pos:sp.Col]), cedar.C_Ident})
			}
			end := sp.Col + sp.Len
			if end > len(runes) {
				end = len(runes)
			}
			if end > sp.Col {
				rs = append(rs, run{string(runes[sp.Col:end]), sp.Cat})
				pos = end
			}
		}
		if pos < len(runes) {
			rs = append(rs, run{string(runes[pos:]), cedar.C_Ident})
		}
		if len(rs) == 0 {
			rs = append(rs, run{" ", cedar.C_Ident})
		}
		out[r] = rs
	}
	return out
}
