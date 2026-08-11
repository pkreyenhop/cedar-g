package main

import (
	"image"
	"image/color"
	"strings"
	"unicode"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"

	"cedarg/internal/tioga"
)

// A Tioga table is stored as one text block whose rows are separated by newlines
// and whose columns are reached with tabs. Rows use a varying number of tabs
// (and padding spaces) to reach the same visual column via the document's tab
// ruler, which the format does not expose — so approximating tabs with fixed
// stops leaves the columns ragged. Instead we split each row into cells on
// tab-runs (which absorbs the varying tab counts and their padding) and lay the
// cells out as a real grid with per-column widths and alignment.

// tabSource returns the block text that still has its tabs: the concatenated
// run text when the block carries looks (runs are never tab-expanded), else the
// block's Text. This matters because the viewer expands tabs in Block.Text for
// the prose path, which would otherwise hide a table from looksLikeTable.
func tabSource(b tioga.Block) string {
	if len(b.Runs) == 0 {
		return b.Text
	}
	var sb strings.Builder
	for _, r := range b.Runs {
		sb.WriteString(r.Text)
	}
	return sb.String()
}

// looksLikeTable reports whether a block is a tab-aligned table: two or more of
// its lines contain a tab.
func looksLikeTable(b tioga.Block) bool {
	src := tabSource(b)
	if !strings.ContainsRune(src, '\t') {
		return false
	}
	n := 0
	for _, ln := range strings.Split(src, "\n") {
		if strings.ContainsRune(ln, '\t') {
			n++
			if n >= 2 {
				return true
			}
		}
	}
	return false
}

// tcell is one grid cell: its text split into look-runs (bold/italic preserved).
type tcell struct{ runs []tioga.Run }

func (c tcell) text() string {
	var sb strings.Builder
	for _, r := range c.runs {
		sb.WriteString(r.Text)
	}
	return sb.String()
}

func (c tcell) empty() bool { return strings.TrimSpace(c.text()) == "" }

type charLook struct {
	r    rune
	look tioga.Look
}

// tableGrid splits a block's runs into rows and cells. A separator is a maximal
// run of tabs and spaces containing at least one tab; a space-only run is
// content (so internal label spaces and leading indent survive). Looks are
// carried onto each cell fragment.
func tableGrid(runs []tioga.Run) [][]tcell {
	var cs []charLook
	for _, rn := range runs {
		for _, r := range rn.Text {
			cs = append(cs, charLook{r, rn.Look})
		}
	}

	var grid [][]tcell
	var row []tcell
	var cell []charLook
	emitCell := func() {
		row = append(row, mergeCell(cell))
		cell = nil
	}
	emitRow := func() {
		emitCell()
		for len(row) > 0 && row[len(row)-1].empty() { // drop trailing empty cells
			row = row[:len(row)-1]
		}
		grid = append(grid, row)
		row = nil
	}

	for i := 0; i < len(cs); {
		c := cs[i].r
		switch {
		case c == '\n':
			emitRow()
			i++
		case c == ' ' || c == '\t':
			j, hasTab := i, false
			for j < len(cs) && (cs[j].r == ' ' || cs[j].r == '\t') {
				if cs[j].r == '\t' {
					hasTab = true
				}
				j++
			}
			if hasTab {
				emitCell()
			} else {
				cell = append(cell, cs[i:j]...) // spaces only: keep as content
			}
			i = j
		default:
			cell = append(cell, cs[i])
			i++
		}
	}
	emitRow()
	return grid
}

// mergeCell trims trailing spaces and coalesces adjacent same-look runes.
func mergeCell(cs []charLook) tcell {
	for len(cs) > 0 && cs[len(cs)-1].r == ' ' {
		cs = cs[:len(cs)-1]
	}
	var runs []tioga.Run
	for _, cl := range cs {
		if n := len(runs); n > 0 && runs[n-1].Look == cl.look {
			runs[n-1].Text += string(cl.r)
		} else {
			runs = append(runs, tioga.Run{Text: string(cl.r), Look: cl.look})
		}
	}
	return tcell{runs}
}

// isNumericText reports whether a cell reads as a number (digits plus the usual
// numeric punctuation) so its column can be right-aligned.
func isNumericText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	digit := false
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digit = true
		case strings.ContainsRune("%.,()+-/ ", r):
		default:
			return false
		}
	}
	return digit
}

// tableBlock lays a table block out as an aligned grid.
func (s *gioUI) tableBlock(gtx C, b tioga.Block) D {
	st := docStyle(b.Format, b.Depth)
	runs := b.Runs
	if len(runs) == 0 { // a table with no looks: split from its (tab-bearing) text
		runs = []tioga.Run{{Text: b.Text}}
	}
	grid := tableGrid(runs)

	ncol := 0
	for _, row := range grid {
		if len(row) > ncol {
			ncol = len(row)
		}
	}
	if ncol == 0 {
		return D{}
	}

	// Column widths and per-column numeric test (right-align numeric columns).
	colW := make([]int, ncol)
	numHits := make([]int, ncol)
	numTot := make([]int, ncol)
	for _, row := range grid {
		for c, cell := range row {
			if w := s.cellWidth(gtx, cell, st.fnt, st.size); w > colW[c] {
				colW[c] = w
			}
			if !cell.empty() {
				numTot[c]++
				if isNumericText(cell.text()) {
					numHits[c]++
				}
			}
		}
	}
	rightAlign := make([]bool, ncol)
	for c := 0; c < ncol; c++ {
		// The first column holds labels; later columns right-align when numeric.
		rightAlign[c] = c > 0 && numTot[c] > 0 && numHits[c]*2 >= numTot[c]
	}

	gap := gtx.Dp(16)
	lineH := s.tokMeasure(gtx, st.fnt, st.size, "Ag").Size.Y
	pitch := lineH + gtx.Dp(3)

	total := 0
	for c := 0; c < ncol; c++ {
		total += colW[c]
		if c < ncol-1 {
			total += gap
		}
	}

	return layout.Inset{Top: st.above, Bottom: st.below}.Layout(gtx, func(gtx C) D {
		y := 0
		for _, row := range grid {
			x := 0
			for c := 0; c < ncol; c++ {
				if c < len(row) {
					cell := row[c]
					cx := x
					if rightAlign[c] {
						cx = x + colW[c] - s.cellWidth(gtx, cell, st.fnt, st.size)
					}
					off := op.Offset(image.Pt(cx, y)).Push(gtx.Ops)
					s.cellDraw(gtx, cell, st.fnt, st.size, cedarBlack)
					off.Pop()
				}
				x += colW[c] + gap
			}
			y += pitch
		}
		return D{Size: image.Pt(total, y)}
	})
}

// cellWidth measures a cell's rendered width (fragments in their look fonts).
func (s *gioUI) cellWidth(gtx C, cell tcell, base font.Font, size float32) int {
	w := 0
	for _, r := range cell.runs {
		w += s.tokMeasure(gtx, fragFont(base, r.Look), size, r.Text).Size.X
	}
	return w
}

// cellDraw paints a cell's fragments left to right at the current origin.
func (s *gioUI) cellDraw(gtx C, cell tcell, base font.Font, size float32, col color.NRGBA) {
	x := 0
	for _, r := range cell.runs {
		off := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		w := s.drawFrag(gtx, r.Text, fragFont(base, r.Look), size, col)
		off.Pop()
		x += w
	}
}

// drawFrag draws one styled fragment and returns its advance width. The layout
// is unbounded so the returned width is the text's own width (not the full cell
// constraint), which is what the caller advances by.
func (s *gioUI) drawFrag(gtx C, txt string, fnt font.Font, size float32, col color.NRGBA) int {
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max = image.Pt(1<<20, 1<<20)
	macro := op.Record(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	cl := macro.Stop()
	return widget.Label{MaxLines: 1, Alignment: text.Start}.Layout(gtx, s.sh, fnt, s.sp(size), txt, cl).Size.X
}

// fragFont applies a run's looks (bold/italic, and the "k" inline-code look) to
// the base face.
func fragFont(base font.Font, lk tioga.Look) font.Font {
	f := base
	if lk.Bold() {
		f.Weight = font.Bold
	}
	if lk.Italic() {
		f.Style = font.Italic
	}
	if lk.Has('k') {
		f.Typeface = "Mono"
	}
	return f
}
