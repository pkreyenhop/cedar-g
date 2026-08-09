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
	// addCollection loads all faces from a .ttc, re-tagging them under typeface.
	addCollection := func(path, typeface string) bool {
		b, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		fs, err := opentype.ParseCollection(b)
		if err != nil || len(fs) == 0 {
			return false
		}
		for i := range fs {
			fs[i].Font.Typeface = font.Typeface(typeface)
			faces = append(faces, fs[i])
		}
		return true
	}

	const mac = "/System/Library/Fonts/Supplemental/"
	// Serif body font (Georgia, a heavy screen serif; DejaVu Serif on Linux).
	add(mac+"Georgia.ttf", "Serif", font.Regular, font.Normal)
	add("/usr/share/fonts/truetype/dejavu/DejaVuSerif.ttf", "Serif", font.Regular, font.Normal)
	add(mac+"Georgia Bold.ttf", "Serif", font.Regular, font.Bold)
	add("/usr/share/fonts/truetype/dejavu/DejaVuSerif-Bold.ttf", "Serif", font.Regular, font.Bold)
	add(mac+"Georgia Italic.ttf", "Serif", font.Italic, font.Normal)
	add(mac+"Georgia Bold Italic.ttf", "Serif", font.Italic, font.Bold)

	// Monospace code font: prefer Menlo (a heavier, very readable mono with all
	// four faces), falling back to Courier New / DejaVu Sans Mono.
	if !addCollection("/System/Library/Fonts/Menlo.ttc", "Mono") &&
		!addCollection("/System/Library/Fonts/PTMono.ttc", "Mono") {
		add(mac+"Courier New.ttf", "Mono", font.Regular, font.Normal)
		add("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf", "Mono", font.Regular, font.Normal)
		add("/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf", "Mono", font.Regular, font.Bold)
		add(mac+"Courier New Bold.ttf", "Mono", font.Regular, font.Bold)
		add(mac+"Courier New Italic.ttf", "Mono", font.Italic, font.Normal)
		add(mac+"Courier New Bold Italic.ttf", "Mono", font.Italic, font.Bold)
	}
	return text.NewShaper(text.WithCollection(faces))
}

// sp scales a base text size by the current zoom.
func (s *gioUI) sp(v float32) unit.Sp { return unit.Sp(v * s.scale) }

// label draws a single string with the given face/style/colour. maxLines 0 wraps.
func (s *gioUI) label(gtx C, base font.Font, weight font.Weight, style font.Style, size float32, txt string, col color.NRGBA, maxLines int) D {
	return s.labelLH(gtx, base, weight, style, size, txt, col, maxLines, 0)
}

// labelLH is label with an explicit line-height scale (0 = default). A value
// above 1 opens up the leading, making wrapped paragraphs more readable.
func (s *gioUI) labelLH(gtx C, base font.Font, weight font.Weight, style font.Style, size float32, txt string, col color.NRGBA, maxLines int, lineHeight float32) D {
	fnt := base
	fnt.Weight = weight
	fnt.Style = style
	macro := op.Record(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	cl := macro.Stop()
	l := widget.Label{MaxLines: maxLines, LineHeightScale: lineHeight}
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

// scroller is a scrollable list with a Cedar-style scrollbar: on the left, wide,
// and only visible while the pointer is over the far-left gutter (or dragging).
type scroller struct {
	list widget.List
}

// scrollGutterDp is the reserved left column the scrollbar lives in. The bar is
// laid out there every frame (so it never flickers and drag always works); it is
// simply painted transparent until the track is hovered.
const scrollGutterDp = 16

func (s *gioUI) scrollList(gtx C, sc *scroller, n int, el layout.ListElement) D {
	sc.list.Axis = layout.Vertical
	full := gtx.Constraints.Max
	gutter := gtx.Dp(scrollGutterDp)
	if gutter > full.X {
		gutter = full.X
	}

	// Content, shifted right of the reserved gutter so the bar never covers text.
	cgtx := gtx
	cgtx.Constraints.Max.X = full.X - gutter
	cgtx.Constraints.Min = cgtx.Constraints.Max
	trans := op.Offset(image.Pt(gutter, 0)).Push(gtx.Ops)
	dims := sc.list.List.Layout(cgtx, n, el)
	trans.Pop()

	totalH := dims.Size.Y
	if totalH == 0 {
		totalH = full.Y
	}

	// Always lay out the scrollbar in the gutter; visible only when hovered/dragged.
	start, end := viewportFraction(sc.list.Position, n, totalH)
	bar := material.Scrollbar(s.th, &sc.list.Scrollbar)
	bar.Indicator.MinorWidth = unit.Dp(scrollGutterDp)
	bar.Indicator.CornerRadius = 0
	bar.Track.MajorPadding = 0
	bar.Track.MinorPadding = 0
	if sc.list.Scrollbar.TrackHovered() || sc.list.Scrollbar.IndicatorHovered() || sc.list.Scrollbar.Dragging() {
		bar.Indicator.Color = cedarGreyMid
		bar.Indicator.HoverColor = cedarBlack
		bar.Track.Color = cedarGrey
	} else {
		clear := color.NRGBA{}
		bar.Indicator.Color, bar.Indicator.HoverColor, bar.Track.Color = clear, clear, clear
	}
	bgtx := gtx
	bgtx.Constraints.Min = image.Pt(gutter, totalH)
	bgtx.Constraints.Max = image.Pt(gutter, totalH)
	layout.W.Layout(bgtx, func(gtx C) D {
		return bar.Layout(gtx, layout.Vertical, start, end)
	})

	if d := sc.list.ScrollDistance(); d != 0 {
		sc.list.List.ScrollBy(d * float32(n))
	}
	return D{Size: image.Pt(full.X, totalH)}
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// viewportFraction computes the scrollbar indicator span [start,end] from a
// list Position (a port of material's unexported fromListPosition).
func viewportFraction(lp layout.Position, elements, majorAxisSize int) (start, end float32) {
	if elements == 0 || lp.Length == 0 {
		return 0, 1
	}
	lengthEstPx := float32(lp.Length)
	elementLenEstPx := lengthEstPx / float32(elements)
	viewportStart := clamp01((float32(lp.First)*elementLenEstPx + float32(lp.Offset)) / lengthEstPx)
	viewportEnd := clamp01((float32(lp.First+lp.Count)*elementLenEstPx + float32(lp.OffsetLast)) / lengthEstPx)
	viewportFrac := viewportEnd - viewportStart
	visibleFraction := float32(majorAxisSize) / lengthEstPx
	err := visibleFraction - viewportFrac
	adjStart, adjEnd := viewportStart, viewportEnd
	if viewportFrac < 1 {
		adjStart -= (viewportStart / (1 - viewportFrac)) * err
		adjEnd += ((1 - viewportEnd) / (1 - viewportFrac)) * err
	}
	return adjStart, adjEnd
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
