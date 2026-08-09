// Command tiogagio is a Gio (immediate-mode) spike of the Cedar/Tioga viewer,
// evaluating Gio as an alternative to the Fyne UI. It reuses the toolkit-neutral
// internal/tioga decoder and internal/cedar highlighter unchanged, and shows the
// parts that were awkward in Fyne: a pixel-precise monochrome look and a real
// tiled column with a draggable boundary between stacked viewers.
package main

import (
	"image"
	"image/color"
	"os"
	"sort"
	"strings"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"cedarg/internal/cedar"
	"cedarg/internal/tioga"
)

type (
	C = layout.Context
	D = layout.Dimensions
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

// run is a styled slice of a code line.
type run struct {
	text string
	cat  cedar.Category
}

type gviewer struct {
	path   string
	isCode bool
	lines  [][]run       // code, precomputed once
	blocks []tioga.Block // documents
	list   layout.List

	grow, destroy, icon, sw, split widget.Clickable
}

type spike struct {
	sh       *text.Shaper
	builtins map[string]bool

	viewers []*gviewer
	weight  float32 // top viewer's fraction of the column
	grown   int     // -1 none, else index maximised
	drag    struct {
		active bool
	}
}

func main() {
	sp := &spike{sh: loadShaper(), builtins: map[string]bool{}, weight: 0.5, grown: -1}
	for _, b := range builtinList {
		sp.builtins[b] = true
	}
	for _, path := range os.Args[1:] {
		sp.viewers = append(sp.viewers, sp.newViewer(path))
	}
	if len(sp.viewers) == 0 {
		// Nothing passed: show a hint viewer.
		sp.viewers = append(sp.viewers, &gviewer{path: "(pass files as arguments)", blocks: []tioga.Block{{Kind: tioga.Paragraph, Text: "Pass .mesa/.tioga files as arguments."}}})
	}
	for len(sp.viewers) < 2 {
		sp.viewers = append(sp.viewers, sp.viewers[len(sp.viewers)-1])
	}

	go func() {
		w := new(app.Window)
		w.Option(app.Title("Cedar Viewers (Gio spike)"), app.Size(unit.Dp(1100), unit.Dp(780)))
		if err := sp.loop(w); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func (s *spike) loop(w *app.Window) error {
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			s.update(gtx)
			s.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// update processes button clicks (event handling for the divider is inline).
func (s *spike) update(gtx C) {
	for i, v := range s.viewers {
		if v.grow.Clicked(gtx) {
			if s.grown == i {
				s.grown = -1
			} else {
				s.grown = i
			}
		}
		v.destroy.Clicked(gtx)
		v.icon.Clicked(gtx)
		v.sw.Clicked(gtx)
		v.split.Clicked(gtx)
	}
}

func (s *spike) layout(gtx C) D {
	// White background.
	fill(gtx, cedarWhite, gtx.Constraints.Max)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(s.globalBar),
		layout.Flexed(1, s.column),
	)
}

// globalBar is the black system row with a title and a Mono toggle.
func (s *spike) globalBar(gtx C) D {
	h := gtx.Dp(26)
	gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarBlack, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.UniformInset(6).Layout(gtx, func(gtx C) D {
						return s.text(gtx, serifFont, font.Bold, font.Regular, 13, "Cedar  Viewers", cedarWhite, 1)
					})
				}),
				layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Max.X, 1)} }),
				layout.Rigid(func(gtx C) D {
					return layout.UniformInset(6).Layout(gtx, func(gtx C) D {
						return s.text(gtx, serifFont, font.Normal, font.Regular, 12, "Gio spike", cedarWhite, 1)
					})
				}),
			)
		}),
	)
}

// column lays out the two stacked viewers with a draggable divider, and handles
// the divider drag. The pointer input covers the whole column (a fixed origin),
// so dragging is stable regardless of the boundary moving.
func (s *spike) column(gtx C) D {
	H := gtx.Constraints.Max.Y
	dividerH := gtx.Dp(6)

	// Handle drag events registered last frame against &s.drag.
	divY := int(s.weight * float32(H))
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &s.drag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			if abs(int(pe.Position.Y)-divY) <= gtx.Dp(8) {
				s.drag.active = true
			}
		case pointer.Drag:
			if s.drag.active {
				s.weight = clampf(pe.Position.Y/float32(H), 0.08, 0.92)
			}
		case pointer.Release, pointer.Cancel:
			s.drag.active = false
		}
	}

	if s.grown >= 0 && s.grown < len(s.viewers) {
		return s.viewer(gtx, s.viewers[s.grown], s.grown)
	}

	topH := int(s.weight * float32(H-dividerH))
	return layout.Stack{}.Layout(gtx,
		// Full-column input area underneath (fixed origin for stable dragging).
		layout.Expanded(func(gtx C) D {
			sz := gtx.Constraints.Min
			defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, &s.drag)
			pointer.CursorRowResize.Add(gtx.Ops)
			return D{Size: sz}
		}),
		layout.Stacked(func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return sized(gtx, gtx.Constraints.Max.X, topH, func(gtx C) D {
						return s.viewer(gtx, s.viewers[0], 0)
					})
				}),
				layout.Rigid(func(gtx C) D { return s.divider(gtx, dividerH) }),
				layout.Flexed(1, func(gtx C) D { return s.viewer(gtx, s.viewers[1], 1) }),
			)
		}),
	)
}

func (s *spike) divider(gtx C, h int) D {
	sz := image.Pt(gtx.Constraints.Max.X, h)
	fill(gtx, cedarGreyMid, sz)
	// thin black rules top and bottom
	fillAt(gtx, cedarBlack, image.Rect(0, 0, sz.X, 1))
	fillAt(gtx, cedarBlack, image.Rect(0, h-1, sz.X, h))
	return D{Size: sz}
}

// viewer draws one bordered Viewer: caption (buttons + title), rule, content.
func (s *spike) viewer(gtx C, v *gviewer, idx int) D {
	return bordered(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return s.caption(gtx, v, idx) }),
			layout.Rigid(func(gtx C) D { return hrule(gtx) }),
			layout.Flexed(1, func(gtx C) D { return s.content(gtx, v) }),
		)
	})
}

// caption is the grey header strip: action buttons then the title.
func (s *spike) caption(gtx C, v *gviewer, idx int) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.UniformInset(2).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.destroy, "Destroy") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.grow, "Grow") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.icon, "Icon") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.sw, "Switch") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.split, "Split") }),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(6), 0)} }),
					layout.Flexed(1, func(gtx C) D {
						return s.text(gtx, serifFont, font.Bold, font.Regular, 13, v.path, cedarBlack, 1)
					}),
				)
			})
		}),
	)
}

// content renders the viewer body: highlighted code or a serif document.
func (s *spike) content(gtx C, v *gviewer) D {
	gtx.Constraints.Min = gtx.Constraints.Max
	return layout.UniformInset(4).Layout(gtx, func(gtx C) D {
		if v.isCode {
			v.list.Axis = layout.Vertical
			return v.list.Layout(gtx, len(v.lines), func(gtx C, i int) D {
				return s.codeLine(gtx, v.lines[i])
			})
		}
		v.list.Axis = layout.Vertical
		return v.list.Layout(gtx, len(v.blocks), func(gtx C, i int) D {
			return s.docBlock(gtx, v.blocks[i])
		})
	})
}

func (s *spike) codeLine(gtx C, runs []run) D {
	children := make([]layout.FlexChild, len(runs))
	for i := range runs {
		r := runs[i]
		col, bold, ital := styleFor(r.cat)
		style, weight := font.Regular, font.Normal
		if ital {
			style = font.Italic
		}
		if bold {
			weight = font.Bold
		}
		children[i] = layout.Rigid(func(gtx C) D {
			return s.text(gtx, monoFont, weight, style, 13, r.text, col, 1)
		})
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
}

func (s *spike) docBlock(gtx C, b tioga.Block) D {
	fnt, weight, style, size, col := serifFont, font.Normal, font.Regular, unit.Sp(16), cedarBlack
	switch b.Kind {
	case tioga.Heading:
		weight = font.Bold
		if b.Level <= 1 {
			size = 26
		} else {
			size = 20
		}
	case tioga.Quote:
		style = font.Italic
	case tioga.Code:
		fnt, size = monoFont, 13
	}
	return layout.Inset{Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
		return s.text(gtx, fnt, weight, style, size, b.Text, col, 0)
	})
}

// ---- primitives ----

// text lays out a string with the given face/colour. maxLines 0 wraps.
func (s *spike) text(gtx C, base font.Font, weight font.Weight, style font.Style, size unit.Sp, txt string, col color.NRGBA, maxLines int) D {
	fnt := base
	fnt.Weight = weight
	fnt.Style = style
	macro := op.Record(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	cl := macro.Stop()
	l := widget.Label{MaxLines: maxLines}
	return l.Layout(gtx, s.sh, fnt, size, txt, cl)
}

// flatButton is a square grey command button with a 1px black border.
func (s *spike) flatButton(gtx C, btn *widget.Clickable, label string) D {
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
						return s.text(gtx, serifFont, font.Normal, font.Regular, 12, label, cedarBlack, 1)
					})
				}),
			)
		})
	})
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

// ---- model helpers ----

func (s *spike) newViewer(path string) *gviewer {
	v := &gviewer{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		v.blocks = []tioga.Block{{Kind: tioga.Paragraph, Text: "cannot open: " + path}}
		return v
	}
	if strings.HasSuffix(path, ".mesa") || strings.Contains(path, ".mesa!") {
		v.isCode = true
		doc := tioga.Read(data, true)
		v.lines = codeToRuns(doc.Code, s.builtins)
	} else {
		doc := tioga.Read(data, false)
		v.blocks = doc.Blocks
	}
	return v
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
			rs = append(rs, run{" ", cedar.C_Ident}) // keep empty lines tall
		}
		out[r] = rs
	}
	return out
}

// styleFor returns the emphasis for a category: always black text, distinguished
// by bold/italic only (Tioga's monochrome "looks").
func styleFor(cat cedar.Category) (color.NRGBA, bool, bool) {
	st := cedar.CategoryStyleMono(cat)
	return cedarBlack, st.Bold, st.Italic
}

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
	add(mac+"Georgia Bold.ttf", "Serif", font.Regular, font.Bold)
	add(mac+"Georgia Italic.ttf", "Serif", font.Italic, font.Normal)
	add(mac+"Georgia Bold Italic.ttf", "Serif", font.Italic, font.Bold)
	add(mac+"Courier New.ttf", "Mono", font.Regular, font.Normal)
	add(mac+"Courier New Bold.ttf", "Mono", font.Regular, font.Bold)
	add(mac+"Courier New Italic.ttf", "Mono", font.Italic, font.Normal)
	add(mac+"Courier New Bold Italic.ttf", "Mono", font.Italic, font.Bold)
	return text.NewShaper(text.WithCollection(faces))
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
