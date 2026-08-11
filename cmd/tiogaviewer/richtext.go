package main

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"

	"cedarg/internal/tioga"
)

// richText lays out a run of styled text with word wrapping, honouring each
// run's looks (bold/italic/underline). It is Tioga's monochrome model: emphasis
// is conveyed by weight/slant/underline, never colour. All runs share one size,
// so their baselines align and mixed styling sits on one line cleanly.
//
// Wrapping is greedy at whitespace; a word that itself spans several runs (a
// look change with no intervening space) is kept together. Embedded newlines are
// hard breaks.
func (s *gioUI) richText(gtx C, runs []tioga.Run, base font.Font, size float32, col color.NRGBA, lineHeight float32, align text.Alignment) D {
	maxW := gtx.Constraints.Max.X

	toks := s.buildTokens(gtx, runs, base, size)
	tabStop := s.tokMeasure(gtx, base, size, "0").Size.X * tabStopEms
	if tabStop <= 0 {
		tabStop = 40
	}
	lines := wrapTokens(toks, maxW, tabStop)

	lineH := s.tokMeasure(gtx, base, size, "Ag").Size.Y
	gap := int(float32(gtx.Sp(s.sp(size))) * (lineHeight - 1))
	if gap < 0 {
		gap = 0
	}

	y := 0
	for li, line := range lines {
		if li > 0 {
			y += lineH + gap
		}
		x := lineStartX(line, maxW, align)
		for _, t := range line {
			off := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
			s.tokDraw(gtx, t, col)
			off.Pop()
			x += t.w
		}
	}
	h := 0
	if len(lines) > 0 {
		h = y + lineH
	}
	return D{Size: image.Pt(maxW, h)}
}

// lineStartX returns the x offset a line starts at for the given alignment,
// centring or right-aligning by the line's rendered width (trailing spaces on a
// wrapped line were already trimmed).
func lineStartX(line []tok, maxW int, align text.Alignment) int {
	if align == text.Start {
		return 0
	}
	w := 0
	for _, t := range line {
		w += t.w
	}
	switch align {
	case text.Middle:
		if x := (maxW - w) / 2; x > 0 {
			return x
		}
	case text.End:
		if x := maxW - w; x > 0 {
			return x
		}
	}
	return 0
}

// hasLooks reports whether any run carries a look worth styling. (A renderer can
// skip richText and draw plain wrapped text when this is false.)
func hasLooks(runs []tioga.Run) bool {
	for _, r := range runs {
		if r.Look != 0 {
			return true
		}
	}
	return false
}

type segKind int

const (
	segWord segKind = iota
	segSpace
	segBreak
	segTab
)

// tabStopEms sets the tab-stop interval as a multiple of the figure ("0") width.
// A tab advances to the next stop, so value columns line up across rows with the
// common tab count. The document's true (non-uniform) tab ruler is not exposed by
// the format, so this uniform approximation cannot align every row exactly; 8
// figures keeps a typical table's rows on one line while lining up most columns.
const tabStopEms = 8

// tok is one measured, styled, non-breaking piece of text.
type tok struct {
	text string
	fnt  font.Font
	size float32
	ul   bool
	w    int
	kind segKind
}

// buildTokens flattens runs into measured tokens, splitting each run at
// whitespace and newlines so wrapTokens can break between them.
func (s *gioUI) buildTokens(gtx C, runs []tioga.Run, base font.Font, size float32) []tok {
	var toks []tok
	for _, r := range runs {
		fnt := base
		if r.Look.Bold() {
			fnt.Weight = font.Bold
		}
		if r.Look.Italic() {
			fnt.Style = font.Italic
		}
		// The "k" look is the default Cedar style's inline code look: fixed-pitch
		// (e.g. FORK/JOIN set in mono within running prose).
		if r.Look.Has('k') {
			fnt.Typeface = "Mono"
		}
		ul := r.Look.Underline()
		for _, seg := range splitSegments(r.Text) {
			// A tab is a zero-glyph token; its width is computed at wrap time as the
			// advance to the next tab stop, so it never draws a missing-glyph box.
			if seg.kind == segTab {
				toks = append(toks, tok{kind: segTab})
				continue
			}
			t := tok{text: seg.text, fnt: fnt, size: size, ul: ul, kind: seg.kind}
			if seg.kind != segBreak {
				t.w = s.tokMeasure(gtx, fnt, size, seg.text).Size.X
			}
			toks = append(toks, t)
		}
	}
	return toks
}

type segment struct {
	text string
	kind segKind
}

// splitSegments divides text into maximal word / space segments, with each
// newline a hard break and each tab its own tab-advance segment.
func splitSegments(text string) []segment {
	var segs []segment
	rs := []rune(text)
	i := 0
	for i < len(rs) {
		switch {
		case rs[i] == '\n':
			segs = append(segs, segment{"\n", segBreak})
			i++
		case rs[i] == '\t':
			segs = append(segs, segment{"\t", segTab})
			i++
		case rs[i] == ' ':
			j := i
			for j < len(rs) && rs[j] == ' ' {
				j++
			}
			segs = append(segs, segment{string(rs[i:j]), segSpace})
			i = j
		default:
			j := i
			for j < len(rs) && rs[j] != '\n' && rs[j] != '\t' && rs[j] != ' ' {
				j++
			}
			segs = append(segs, segment{string(rs[i:j]), segWord})
			i = j
		}
	}
	return segs
}

// wrapTokens greedily packs tokens into lines no wider than maxW. Consecutive
// word tokens (a word split across runs) are measured together so a look change
// never forces a mid-word break. Leading spaces on a wrapped line are dropped;
// a hard break starts a new line.
func wrapTokens(toks []tok, maxW, tabStop int) [][]tok {
	var lines [][]tok
	var cur []tok
	curW := 0
	lineHasTab := false // a tabbed (table) row is kept on one line, never wrapped
	trimTrailing := func() {
		for len(cur) > 0 && cur[len(cur)-1].kind == segSpace {
			curW -= cur[len(cur)-1].w
			cur = cur[:len(cur)-1]
		}
	}
	flush := func() {
		lines = append(lines, cur)
		cur = nil
		curW = 0
		lineHasTab = false
	}

	i := 0
	for i < len(toks) {
		t := toks[i]
		switch t.kind {
		case segBreak:
			trimTrailing()
			flush()
			i++
		case segTab:
			// Advance to the next tab stop measured from the line's left edge, so
			// value columns line up regardless of the (proportional) label width.
			if tabStop <= 0 {
				i++
				continue
			}
			lineHasTab = true
			w := (curW/tabStop+1)*tabStop - curW
			t.w = w
			cur = append(cur, t)
			curW += w
			i++
		case segSpace:
			if len(cur) == 0 { // drop leading whitespace on a line
				i++
				continue
			}
			cur = append(cur, t)
			curW += t.w
			i++
		default: // segWord: gather the whole (possibly multi-run) word
			j, wordW := i, 0
			for j < len(toks) && toks[j].kind == segWord {
				wordW += toks[j].w
				j++
			}
			// A tabbed row is a table line: never fragment it (let it overflow the
			// column rather than splitting a row across lines).
			if !lineHasTab && curW+wordW > maxW && len(cur) > 0 {
				trimTrailing()
				flush()
			}
			for k := i; k < j; k++ {
				cur = append(cur, toks[k])
				curW += toks[k].w
			}
			i = j
		}
	}
	if len(cur) > 0 {
		flush()
	}
	if len(lines) == 0 {
		lines = append(lines, nil) // keep at least one (empty) line
	}
	return lines
}

// tokMeasure shapes txt without drawing, returning its dimensions.
func (s *gioUI) tokMeasure(gtx C, fnt font.Font, size float32, txt string) D {
	g := gtx
	g.Constraints.Min = image.Point{}
	g.Constraints.Max = image.Pt(1<<20, 1<<20)
	discard := op.Record(g.Ops)
	cm := op.Record(g.Ops)
	paint.ColorOp{Color: cedarBlack}.Add(g.Ops)
	cl := cm.Stop()
	d := widget.Label{MaxLines: 1}.Layout(g, s.sh, fnt, s.sp(size), txt, cl)
	discard.Stop() // drop the shaping/draw ops; keep only the measurement
	return d
}

// tokDraw renders one token and its underline (if any).
func (s *gioUI) tokDraw(gtx C, t tok, col color.NRGBA) D {
	cm := op.Record(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	cl := cm.Stop()
	d := widget.Label{MaxLines: 1}.Layout(gtx, s.sh, t.fnt, s.sp(t.size), t.text, cl)
	if t.ul && d.Size.X > 0 {
		uy := d.Size.Y - d.Baseline + 2
		if uy >= d.Size.Y {
			uy = d.Size.Y - 1
		}
		fillAt(gtx, col, image.Rect(0, uy, d.Size.X, uy+1))
	}
	return d
}
