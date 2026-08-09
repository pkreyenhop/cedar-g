package main

import (
	"fmt"
	"image"
	"os"
	"sort"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

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
const termTextSize = 17

// blinkHalf is the on/off half-period of the terminal caret.
const blinkHalf = 500 * time.Millisecond

// run is a styled slice of a code line.
type run struct {
	text string
	cat  cedar.Category
}

type viewerKind int

const (
	vkContent viewerKind = iota // a decoded document or code file
	vkEditor                    // a blank/editable Tioga document
	vkTerm                      // a shell terminal
)

// viewer is one Cedar "Viewer": a pane living in a column.
type viewer struct {
	kind   viewerKind
	path   string
	rel    string
	title  string // header title override (editor/terminal)
	col    int
	isCode bool
	lines  [][]run       // code
	blocks []tioga.Block // documents
	editor widget.Editor // vkEditor
	term   *terminal     // vkTerm
	sc     scroller

	bDestroy, bGrow, bIcon, bSwitch, bSplit widget.Clickable
	bRestore                                widget.Clickable // in the icon tray
	headerHovered                           bool
	termFocus                               int // key/pointer focus tag for the terminal
}

func (v *viewer) headerTitle() string {
	if v.kind == vkTerm && v.term != nil {
		if d := v.term.dir(); d != "" {
			return d
		}
		return "Terminal"
	}
	if v.title != "" {
		return v.title
	}
	return v.rel
}

func (s *gioUI) newViewer(path string) *viewer {
	v := &viewer{path: path, rel: s.relPath(path)}
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

var newDocCount int

// newEditorViewer opens a blank editable document.
func (s *gioUI) newEditorViewer() *viewer {
	newDocCount++
	v := &viewer{kind: vkEditor, title: fmt.Sprintf("New Document %d", newDocCount)}
	return v
}

// newTerminalViewer opens a shell terminal.
func (s *gioUI) newTerminalViewer() *viewer {
	return &viewer{kind: vkTerm, title: "Terminal", term: newTerminal(s.invalidate)}
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
						return s.label(gtx, serifFont, font.Bold, font.Regular, 13, v.headerTitle(), cedarBlack, 1)
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
				return s.label(gtx, serifFont, font.Bold, font.Regular, 13, v.headerTitle(), cedarWhite, 1)
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
	switch v.kind {
	case vkEditor:
		return s.editorBody(gtx, v)
	case vkTerm:
		return s.termBody(gtx, v)
	}
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

// editorBody renders an editable document (a serif multiline editor).
func (s *gioUI) editorBody(gtx C, v *viewer) D {
	gtx.Constraints.Min = gtx.Constraints.Max
	return layout.Inset{Top: 6, Left: 10, Right: 10, Bottom: 6}.Layout(gtx, func(gtx C) D {
		ed := material.Editor(s.th, &v.editor, "Type a new Tioga document…")
		ed.Color = cedarBlack
		ed.HintColor = cedarGreyMid
		ed.SelectionColor = cedarGreyMid
		ed.Font = serifFont
		ed.TextSize = s.sp(docTextSize)
		return ed.Layout(gtx)
	})
}

// termBody renders the shell terminal: output fills the whole body and you type
// straight into it (there is no separate command line). Keystrokes are sent raw
// to the pty, which echoes them, so input appears inline with the output.
func (s *gioUI) termBody(gtx C, v *viewer) D {
	t := v.term
	gtx.Constraints.Min = gtx.Constraints.Max

	lines, crow, ccol := t.snapshot()
	// Follow the newest output, but only bottom-anchor once it overflows the
	// viewport; while the output still fits, keep it top-aligned like a real
	// terminal (OffsetLast is the leftover space from the previous layout).
	v.sc.list.List.ScrollToEnd = v.sc.list.List.Position.OffsetLast <= 0

	// A Cedar-style caret marks the insertion point at the cursor: blinking
	// while focused, steady otherwise.
	caretOn := true
	if gtx.Focused(&v.termFocus) {
		caretOn = (gtx.Now.UnixNano()/int64(blinkHalf))%2 == 0
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(blinkHalf)})
	}
	caretRow := -1
	if caretOn {
		caretRow = crow
	}

	dims := layout.Inset{Top: 4, Right: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = gtx.Constraints.Max
		return s.scrollList(gtx, &v.sc, len(lines), func(gtx C, i int) D {
			caretCol := -1
			if i == caretRow {
				caretCol = ccol
			}
			return s.termLine(gtx, lines[i], caretCol)
		})
	})

	// Full-body input zone on top (pass-through, so the scrollbar/wheel still
	// work): click to focus, then keys go to the shell.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.termFocus)
	key.InputHintOp{Tag: &v.termFocus, Hint: key.HintAny}.Add(gtx.Ops)
	area.Pop()
	pass.Pop()
	s.processTermKeys(gtx, v)

	return dims
}

// processTermKeys forwards keystrokes to the shell. Printable text arrives as
// EditEvents; control keys and Ctrl-combos as key.Events mapped to bytes.
func (s *gioUI) processTermKeys(gtx C, v *viewer) {
	t := v.term
	tag := &v.termFocus
	ctrlNames := []key.Name{
		key.NameReturn, key.NameEnter, key.NameDeleteBackward, key.NameDeleteForward,
		key.NameTab, key.NameEscape, key.NameLeftArrow, key.NameRightArrow,
		key.NameUpArrow, key.NameDownArrow, key.NameHome, key.NameEnd,
		key.NamePageUp, key.NamePageDown,
	}
	filters := []event.Filter{
		key.FocusFilter{Target: tag},
		pointer.Filter{Target: tag, Kinds: pointer.Press},
		// Ctrl-combos (Ctrl-C, Ctrl-D, Ctrl-L, ...) produce no text of their own.
		key.Filter{Focus: tag, Name: "", Required: key.ModCtrl, Optional: key.ModShift | key.ModAlt},
	}
	for _, nm := range ctrlNames {
		filters = append(filters, key.Filter{Focus: tag, Name: nm, Optional: key.ModShift | key.ModCtrl | key.ModAlt})
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case pointer.Event:
			if e.Kind == pointer.Press {
				gtx.Execute(key.FocusCmd{Tag: tag})
			}
		case key.EditEvent:
			t.write(e.Text)
		case key.Event:
			if e.State == key.Press {
				t.write(termBytes(e))
			}
		}
	}
}

// termBytes maps a control key event to the bytes a terminal would send.
func termBytes(e key.Event) string {
	if e.Modifiers.Contain(key.ModCtrl) && len(e.Name) == 1 {
		c := e.Name[0]
		switch {
		case c >= 'A' && c <= 'Z':
			return string([]byte{c - 'A' + 1})
		case c >= 'a' && c <= 'z':
			return string([]byte{c - 'a' + 1})
		}
	}
	switch e.Name {
	case key.NameReturn, key.NameEnter:
		return "\r"
	case key.NameDeleteBackward:
		return "\x7f"
	case key.NameDeleteForward:
		return "\x1b[3~"
	case key.NameTab:
		return "\t"
	case key.NameEscape:
		return "\x1b"
	case key.NameLeftArrow:
		return "\x1b[D"
	case key.NameRightArrow:
		return "\x1b[C"
	case key.NameUpArrow:
		return "\x1b[A"
	case key.NameDownArrow:
		return "\x1b[B"
	case key.NameHome:
		return "\x1b[H"
	case key.NameEnd:
		return "\x1b[F"
	case key.NamePageUp:
		return "\x1b[5~"
	case key.NamePageDown:
		return "\x1b[6~"
	}
	return ""
}

// termLabel draws a run of terminal text.
func (s *gioUI) termLabel(gtx C, text string) D {
	return s.label(gtx, monoFont, font.Normal, font.Regular, termTextSize, text, cedarBlack, 1)
}

// termLine draws one terminal output line. caretCol >= 0 places the insertion
// caret at that rune column (splitting the line around it).
func (s *gioUI) termLine(gtx C, text string, caretCol int) D {
	if caretCol < 0 {
		return s.termLabel(gtx, text)
	}
	r := []rune(text)
	prefix := text
	suffix := ""
	if caretCol < len(r) {
		prefix, suffix = string(r[:caretCol]), string(r[caretCol:])
	} else if caretCol > len(r) {
		prefix = text + strings.Repeat(" ", caretCol-len(r)) // cursor past line end
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return s.termLabel(gtx, prefix) }),
		layout.Rigid(func(gtx C) D { return s.termCaret(gtx) }),
		layout.Rigid(func(gtx C) D { return s.termLabel(gtx, suffix) }),
	)
}

// termCaret draws the Cedar insertion caret: a small, bold up-pointing wedge
// sitting on the text baseline.
func (s *gioUI) termCaret(gtx C) D {
	em := float32(gtx.Sp(s.sp(termTextSize)))
	w := int(em * 0.6)
	h := int(em * 0.44)
	pad := int(em * 0.26) // drop to roughly the baseline
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(0, float32(h)))
	p.LineTo(f32.Pt(float32(w)/2, 0))
	p.LineTo(f32.Pt(float32(w), float32(h)))
	stroke := clip.Stroke{Path: p.End(), Width: em * 0.18}.Op() // bold
	off := op.Offset(image.Pt(0, pad)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, cedarBlack, stroke)
	off.Pop()
	return D{Size: image.Pt(w, h+pad)}
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
