package main

import (
	"image"
	"image/color"
	"os"

	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

// Cedar monochrome palette.
var (
	cedarBlack   = color.NRGBA{0x00, 0x00, 0x00, 0xff}
	cedarWhite   = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	cedarGrey    = color.NRGBA{0xd0, 0xd0, 0xd0, 0xff}
	cedarGreyMid = color.NRGBA{0xa8, 0xa8, 0xa8, 0xff}
)

var (
	serifFont = font.Font{Typeface: "Serif"}
	monoFont  = font.Font{Typeface: "Mono"}
)

// loadShaper builds a serif + monospace collection from system fonts, with
// graceful fallback (macOS paths first, then common Linux locations).
func loadShaper() *text.Shaper {
	var faces []font.FontFace
	add := func(path, typeface string, style font.Style, weight font.Weight) {
		if b, err := os.ReadFile(path); err == nil {
			if f, err := opentype.Parse(b); err == nil {
				faces = append(faces, font.FontFace{Font: font.Font{Typeface: font.Typeface(typeface), Style: style, Weight: weight}, Face: f})
			}
		}
	}
	const mac = "/System/Library/Fonts/Supplemental/"
	add(mac+"Georgia.ttf", "Serif", font.Regular, font.Normal)
	add("/usr/share/fonts/truetype/dejavu/DejaVuSerif.ttf", "Serif", font.Regular, font.Normal)
	add(mac+"Georgia Bold.ttf", "Serif", font.Regular, font.Bold)
	add("/usr/share/fonts/truetype/dejavu/DejaVuSerif-Bold.ttf", "Serif", font.Regular, font.Bold)
	add(mac+"Georgia Italic.ttf", "Serif", font.Italic, font.Normal)
	add(mac+"Georgia Bold Italic.ttf", "Serif", font.Italic, font.Bold)
	add(mac+"Courier New.ttf", "Mono", font.Regular, font.Normal)
	add("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf", "Mono", font.Regular, font.Normal)
	add(mac+"Courier New Bold.ttf", "Mono", font.Regular, font.Bold)
	add(mac+"Courier New Italic.ttf", "Mono", font.Italic, font.Normal)
	add(mac+"Courier New Bold Italic.ttf", "Mono", font.Italic, font.Bold)
	return text.NewShaper(text.WithCollection(faces))
}

// sp scales a base text size by the current zoom.
func (s *gioUI) sp(v float32) unit.Sp { return unit.Sp(v * s.scale) }

// label draws a single string with the given face/style/colour. maxLines 0 wraps.
func (s *gioUI) label(gtx C, base font.Font, weight font.Weight, style font.Style, size float32, txt string, col color.NRGBA, maxLines int) D {
	fnt := base
	fnt.Weight = weight
	fnt.Style = style
	macro := op.Record(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	cl := macro.Stop()
	l := widget.Label{MaxLines: maxLines}
	return l.Layout(gtx, s.sh, fnt, s.sp(size), txt, cl)
}

// flatButton is a square grey command button with a 1px black border.
func (s *gioUI) flatButton(gtx C, btn *widget.Clickable, label string) D {
	return btn.Layout(gtx, func(gtx C) D {
		return bordered(gtx, func(gtx C) D {
			bg := cedarGrey
			if btn.Hovered() {
				bg = cedarGreyMid
			}
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx C) D { return fill(gtx, bg, gtx.Constraints.Min) }),
				layout.Stacked(func(gtx C) D {
					return layout.Inset{Top: 1, Bottom: 1, Left: 5, Right: 5}.Layout(gtx, func(gtx C) D {
						return s.label(gtx, serifFont, font.Normal, font.Regular, 12, label, cedarBlack, 1)
					})
				}),
			)
		})
	})
}

// captionStrip is a grey header strip with a black label (e.g. "Files").
func (s *gioUI) captionStrip(gtx C, txt string) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.UniformInset(3).Layout(gtx, func(gtx C) D {
				return s.label(gtx, serifFont, font.Normal, font.Regular, 12, txt, cedarBlack, 1)
			})
		}),
	)
}

// scrollList lays out a scrollable list with a Cedar-styled scrollbar.
func (s *gioUI) scrollList(gtx C, list *widget.List, n int, el layout.ListElement) D {
	ls := material.List(s.th, list)
	ls.Track.Color = cedarGrey
	ls.Indicator.Color = cedarGreyMid
	ls.Indicator.HoverColor = cedarBlack
	return ls.Layout(gtx, n, el)
}

// bordered wraps a widget in a 1px black frame over a white background.
func bordered(gtx C, w layout.Widget) D {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarBlack, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.UniformInset(1).Layout(gtx, func(gtx C) D {
				return layout.Stack{}.Layout(gtx,
					layout.Expanded(func(gtx C) D { return fill(gtx, cedarWhite, gtx.Constraints.Min) }),
					layout.Stacked(w),
				)
			})
		}),
	)
}

func hrule(gtx C) D {
	sz := image.Pt(gtx.Constraints.Max.X, 1)
	fill(gtx, cedarBlack, sz)
	return D{Size: sz}
}

// fill paints a solid rectangle of the given size at the origin.
func fill(gtx C, c color.NRGBA, sz image.Point) D {
	fillAt(gtx, c, image.Rectangle{Max: sz})
	return D{Size: sz}
}

func fillAt(gtx C, c color.NRGBA, r image.Rectangle) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// sized runs w under fixed constraints.
func sized(gtx C, w, h int, wgt layout.Widget) D {
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	return wgt(gtx)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
