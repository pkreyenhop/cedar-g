package main

import (
	"image"
	"image/color"
	"os"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
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
	cedarSelBg   = color.NRGBA{0xe8, 0xe8, 0xe8, 0xff} // selected-node highlight
	cedarDesktop = color.NRGBA{0x8f, 0x8f, 0x8f, 0xff} // the 50%-gray Cedar root
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

// scroller is a scrollable list with a Cedar-style scrollbar living in a
// reserved left gutter. It shows only while hovered, and scrolls on mouse
// buttons: hold the left button to scroll up, the right button to scroll down.
type scroller struct {
	list      widget.List
	hovered   bool
	scrollDir int // -1 up (left button), +1 down (right button), 0 idle
}

const (
	// scrollGutterDp is the reserved left column the scrollbar occupies.
	scrollGutterDp = 16
	// scrollHoverDp is the wider left band that reveals the scrollbar on hover.
	scrollHoverDp = 52
)

func (s *gioUI) scrollList(gtx C, sc *scroller, n int, el layout.ListElement) D {
	sc.list.Axis = layout.Vertical
	full := gtx.Constraints.Max
	gutter := gtx.Dp(scrollGutterDp)
	if gutter > full.X {
		gutter = full.X
	}
	hoverW := gtx.Dp(scrollHoverDp)
	if hoverW > full.X {
		hoverW = full.X
	}

	// Scrollbar press events (left = up, right = down).
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: sc, Kinds: pointer.Press | pointer.Release})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			switch pe.Kind {
			case pointer.Press:
				// A click jumps to the clicked location (its fraction of the track).
				if full.Y > 0 && n > 0 {
					f := clamp01(float32(pe.Position.Y) / float32(full.Y))
					sc.list.List.Position.First = int(f * float32(n))
					sc.list.List.Position.Offset = 0
				}
				// Cedar button semantics: left holds scrolling up, right down; the
				// middle button just thumbs to the absolute position (no hold).
				switch {
				case pe.Buttons.Contain(pointer.ButtonTertiary):
					sc.scrollDir = 0
				case pe.Buttons.Contain(pointer.ButtonPrimary):
					sc.scrollDir = -1 // left → up
				case pe.Buttons.Contain(pointer.ButtonSecondary):
					sc.scrollDir = 1 // right → down
				}
			case pointer.Release:
				sc.scrollDir = 0
			}
		}
	}
	// Hover-zone events (a wider band, so the bar appears as you approach the left).
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &sc.hovered, Kinds: pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			switch pe.Kind {
			case pointer.Enter:
				sc.hovered = true
			case pointer.Leave:
				sc.hovered = false
				sc.scrollDir = 0
			}
		}
	}

	// While a button is held, scroll slowly and continuously so the position is
	// easy to follow, re-rendering each frame.
	if sc.scrollDir != 0 && n > 0 {
		step := float32(sc.list.Position.Count) * 0.01
		if step < 0.1 {
			step = 0.1
		}
		sc.list.List.ScrollBy(float32(sc.scrollDir) * step)
		gtx.Execute(op.InvalidateCmd{})
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

	// A Cedar scrollbar: the position thumb is always shown on the left edge
	// (thin when idle), and the full grey track appears on hover.
	start, end := viewportFraction(sc.list.Position, n, totalH)
	ty0 := int(clamp01(start) * float32(totalH))
	ty1 := int(clamp01(end) * float32(totalH))
	if ty1 <= ty0 {
		ty1 = ty0 + 2
	}
	thumbW := gutter / 3
	if sc.hovered {
		fillAt(gtx, cedarGrey, image.Rect(0, 0, gutter, totalH))
		thumbW = gutter
	}
	fillAt(gtx, cedarGreyMid, image.Rect(0, ty0, thumbW, ty1))

	// Narrow scrollbar press area (opaque; handles the button scroll).
	pst := clip.Rect{Max: image.Pt(gutter, totalH)}.Push(gtx.Ops)
	event.Op(gtx.Ops, sc)
	pointer.CursorPointer.Add(gtx.Ops)
	pst.Pop()

	// Wider hover band on top, pass-through so it only reveals the bar and never
	// eats clicks or wheel-scroll from the content.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	hst := clip.Rect{Max: image.Pt(hoverW, totalH)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &sc.hovered)
	hst.Pop()
	pass.Pop()

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

// circlePath builds a circle outline path (four cubic segments).
func circlePath(ops *op.Ops, cx, cy, r float32) clip.PathSpec {
	const k = 0.5522847498
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(f32.Pt(cx+r, cy))
	p.CubeTo(f32.Pt(cx+r, cy+r*k), f32.Pt(cx+r*k, cy+r), f32.Pt(cx, cy+r))
	p.CubeTo(f32.Pt(cx-r*k, cy+r), f32.Pt(cx-r, cy+r*k), f32.Pt(cx-r, cy))
	p.CubeTo(f32.Pt(cx-r, cy-r*k), f32.Pt(cx-r*k, cy-r), f32.Pt(cx, cy-r))
	p.CubeTo(f32.Pt(cx+r*k, cy-r), f32.Pt(cx+r, cy-r*k), f32.Pt(cx+r, cy))
	p.Close()
	return p.End()
}

// bullseye draws Cedar's bull's-eye cursor centred at p: a ring, a centre dot and
// a crosshair. Drawn in black with a thin white halo so it reads on any content.
func (s *gioUI) bullseye(gtx C, p f32.Point) {
	cx, cy := p.X, p.Y
	r := float32(gtx.Dp(7))
	arm := float32(gtx.Dp(11))
	ring := func(col color.NRGBA, w float32) {
		paint.FillShape(gtx.Ops, col, clip.Stroke{Path: circlePath(gtx.Ops, cx, cy, r), Width: w}.Op())
	}
	bar := func(col color.NRGBA, x0, y0, x1, y1 float32) {
		fillAt(gtx, col, image.Rect(int(x0), int(y0), int(x1), int(y1)))
	}
	// White halo first, then the black cursor over it.
	ring(cedarWhite, 3)
	bar(cedarWhite, cx-arm-1, cy-2, cx+arm+1, cy+2)
	bar(cedarWhite, cx-2, cy-arm-1, cx+2, cy+arm+1)
	ring(cedarBlack, 1.5)
	bar(cedarBlack, cx-arm, cy, cx+arm, cy+1)
	bar(cedarBlack, cx, cy-arm, cx+1, cy+arm)
	paint.FillShape(gtx.Ops, cedarBlack, clip.Ellipse{Min: image.Pt(int(cx-2), int(cy-2)), Max: image.Pt(int(cx+2), int(cy+2))}.Op(gtx.Ops))
}

// withBullseye lays out content, tracks the pointer over it, hides the OS cursor
// and draws the Cedar bull's-eye following the pointer.
func (s *gioUI) withBullseye(gtx C, v *viewer, content layout.Widget) D {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.ptrPos, Kinds: pointer.Move | pointer.Drag | pointer.Enter | pointer.Leave | pointer.Press})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			switch {
			case pe.Kind == pointer.Leave:
				v.ptrIn = false
			case pe.Kind == pointer.Press && pe.Buttons.Contain(pointer.ButtonSecondary):
				v.menuOpen, v.menuPos = true, pe.Position // right button: pop up the menu
			case pe.Kind == pointer.Press:
				v.menuOpen = false // any other press dismisses it
				v.ptrPos, v.ptrIn = pe.Position, true
			default:
				v.ptrPos, v.ptrIn = pe.Position, true
			}
		}
	}
	dims := content(gtx)
	if v.menuOpen {
		s.popupMenu(gtx, v)
	}
	// Pass-through pointer zone: receive Move without stealing scroll/clicks, and
	// hide the OS cursor so our drawn bull's-eye stands in for it.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.ptrPos)
	pointer.CursorNone.Add(gtx.Ops)
	area.Pop()
	pass.Pop()
	if v.ptrIn {
		s.bullseye(gtx, v.ptrPos)
	}
	return dims
}

// popupMenu draws the right-button context menu at the click position.
func (s *gioUI) popupMenu(gtx C, v *viewer) {
	items := []struct {
		b     *widget.Clickable
		label string
	}{
		{&v.bmLevels, "Levels"},
		{&v.bmFind, "Find…"},
		{&v.bmRefs, "References"},
		{&v.bmEdit, "Edit structure"},
		{&v.bmCopy, "Copy node"},
		{&v.bmPrint, "Print (HTML)"},
		{&v.bmArt, "Artwork on/off"},
		{&v.bmReset, "Reset"},
	}
	// Drawn after the content, so it is on top; the offset positions it at the
	// click. Width is bounded so the buttons fill a tidy menu.
	off := op.Offset(image.Pt(int(v.menuPos.X), int(v.menuPos.Y))).Push(gtx.Ops)
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max.X = gtx.Dp(150)
	bordered(gtx, func(gtx C) D {
		children := make([]layout.FlexChild, len(items))
		for i, it := range items {
			it := it
			children[i] = layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Dp(148)
				return s.flatButton(gtx, it.b, it.label)
			})
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	off.Pop()
}

// processMenu handles the pop-up menu's item clicks.
func (s *gioUI) processMenu(gtx C, v *viewer) {
	act := func(f func()) { f(); v.menuOpen = false }
	switch {
	case v.bmLevels.Clicked(gtx):
		act(func() { v.showLevels = !v.showLevels })
	case v.bmFind.Clicked(gtx):
		act(func() {
			v.findOpen = true
			gtx.Execute(key.FocusCmd{Tag: &v.findEd})
		})
	case v.bmRefs.Clicked(gtx):
		act(func() {
			v.showRefs = !v.showRefs
			if v.showRefs {
				v.computeRefs()
			}
		})
	case v.bmEdit.Clicked(gtx):
		act(func() { s.enterStruct(v) })
	case v.bmPrint.Clicked(gtx):
		act(func() { s.exportDoc(v) })
	case v.bmArt.Clicked(gtx):
		act(func() { s.artworkOff = !s.artworkOff })
	case v.bmCopy.Clicked(gtx):
		act(func() { s.copyNode(gtx, v) })
	case v.bmReset.Clicked(gtx):
		act(func() { s.reloadViewer(v) })
	}
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
