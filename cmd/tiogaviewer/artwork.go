package main

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"cedarg/internal/gargoyle"
)

// artMaxW / artMaxH bound the on-screen size of a rendered figure.
const (
	artMaxW = 560
	artMaxH = 460
	artPad  = 10 // inner padding in px so strokes near the edge are not clipped
)

// scene parses (and caches) the Gargoyle bytes attached to an artwork block.
func (v *viewer) scene(art []byte) *gargoyle.Scene {
	if len(art) == 0 {
		return nil
	}
	key := &art[0]
	if v.artCache == nil {
		v.artCache = map[*byte]*gargoyle.Scene{}
	}
	if sc, ok := v.artCache[key]; ok {
		return sc
	}
	sc := gargoyle.Parse(art)
	v.artCache[key] = sc
	return sc
}

// artworkBlock renders a Gargoyle figure fit into the column width, framed like
// a figure. It falls back to nil dimensions when the scene is empty so the
// caller can show the caption instead.
func (s *gioUI) artworkBlock(gtx C, sc *gargoyle.Scene) (D, bool) {
	if sc == nil || sc.Empty() || sc.Width() <= 0 || sc.Height() <= 0 {
		return D{}, false
	}
	aspect := sc.Width() / sc.Height()

	// Fit a box into the available width, capped, preserving aspect ratio.
	availW := gtx.Constraints.Max.X
	w := availW
	if m := gtx.Dp(artMaxW); w > m {
		w = m
	}
	h := int(float64(w) / aspect)
	if m := gtx.Dp(artMaxH); h > m {
		h = m
		w = int(float64(h) * aspect)
	}
	if w < 1 || h < 1 {
		return D{}, false
	}

	inner := image.Pt(w-2*artPad, h-2*artPad)
	scale := math.Min(float64(inner.X)/sc.Width(), float64(inner.Y)/sc.Height())
	toPx := func(p gargoyle.Point) f32.Point {
		return f32.Pt(
			float32(artPad+(p.X-sc.Min.X)*scale),
			float32(artPad+(sc.Max.Y-p.Y)*scale), // flip Y (scene is Y-up)
		)
	}

	return layout.Inset{Top: 6, Bottom: 10}.Layout(gtx, func(gtx C) D {
		// White frame with a thin border, so figures read as figures.
		fillAt(gtx, cedarWhite, image.Rect(0, 0, w, h))
		border(gtx, image.Rect(0, 0, w, h), cedarGreyMid)

		clipStack := clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops)
		s.drawScene(gtx, sc, scale, toPx)
		clipStack.Pop()
		return D{Size: image.Pt(w, h)}
	}), true
}

// drawScene paints the scene's paths and labels through the scene→pixel map.
func (s *gioUI) drawScene(gtx C, sc *gargoyle.Scene, scale float64, toPx func(gargoyle.Point) f32.Point) {
	for _, pa := range sc.Paths {
		if len(pa.Pts) == 0 {
			continue
		}
		var path clip.Path
		path.Begin(gtx.Ops)
		path.MoveTo(toPx(pa.Pts[0]))
		for _, pt := range pa.Pts[1:] {
			path.LineTo(toPx(pt))
		}
		if pa.Closed || pa.Filled {
			path.Close()
		}
		spec := path.End()
		if pa.Filled {
			paint.FillShape(gtx.Ops, nrgba(pa.Fill), clip.Outline{Path: spec}.Op())
			continue
		}
		width := float32(pa.Width * scale)
		if width < 1 {
			width = 1
		}
		paint.FillShape(gtx.Ops, nrgba(pa.Stroke), clip.Stroke{Path: spec, Width: width}.Op())
	}
	for _, l := range sc.Labels {
		org := toPx(gargoyle.Point{X: l.X, Y: l.Y})
		px := float32(l.Size * scale)
		if px < 4 {
			px = 4
		}
		// Scene origin is the text baseline; our label draws from the top, so lift
		// it by roughly the ascent.
		off := op.Offset(image.Pt(int(org.X), int(org.Y-px*0.82))).Push(gtx.Ops)
		s.drawTextPx(gtx, l.Text, px, l.Italic, nrgba(l.Color))
		off.Pop()
	}
}

// drawTextPx draws a single line of text at an explicit pixel size (bypassing
// the document zoom, since the figure is already fit to pixels).
func (s *gioUI) drawTextPx(gtx C, txt string, px float32, italic bool, col color.NRGBA) {
	fnt := serifFont
	if italic {
		fnt.Style = font.Italic
	}
	macro := op.Record(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	cl := macro.Stop()
	sp := unit.Sp(px)
	if gtx.Metric.PxPerSp > 0 {
		sp = unit.Sp(px / gtx.Metric.PxPerSp)
	}
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max = image.Pt(1<<20, 1<<20)
	widget.Label{MaxLines: 1}.Layout(gtx, s.sh, fnt, sp, txt, cl)
}

// border strokes a 1px rectangle outline in the given colour.
func border(gtx C, r image.Rectangle, c color.NRGBA) {
	fillAt(gtx, c, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+1))
	fillAt(gtx, c, image.Rect(r.Min.X, r.Max.Y-1, r.Max.X, r.Max.Y))
	fillAt(gtx, c, image.Rect(r.Min.X, r.Min.Y, r.Min.X+1, r.Max.Y))
	fillAt(gtx, c, image.Rect(r.Max.X-1, r.Min.Y, r.Max.X, r.Max.Y))
}

func nrgba(c gargoyle.Color) color.NRGBA {
	cl := func(f float32) uint8 {
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		return uint8(f*255 + 0.5)
	}
	a := c.A
	if a == 0 {
		a = 1
	}
	return color.NRGBA{R: cl(c.R), G: cl(c.G), B: cl(c.B), A: cl(a)}
}
