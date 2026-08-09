// Command tiogaviewer is the Gio (immediate-mode) Cedar/Mesa TiogaViewer.
// It reuses the toolkit-neutral internal/tioga decoder and
// internal/cedar highlighter, and implements Cedar's tiled "Viewers" model:
// two columns of stacked, height-partitioned viewers with draggable boundaries,
// header actions (Destroy/Grow/Icon/Switch/Split), an icon tray, a file tree,
// and a monochrome black-on-white look (highlighting via bold/italic only).
package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type gioUI struct {
	sh       *text.Shaper
	th       *material.Theme
	builtins map[string]bool
	scale    float32

	root string
	tree *tree

	cols      [numColumns]*column
	minimized []*viewer

	bUp, bZoomIn, bZoomOut, bZoomReset widget.Clickable

	keyTag  int
	focused bool
}

func newUI() *gioUI {
	s := &gioUI{
		sh:       loadShaper(),
		builtins: map[string]bool{},
		scale:    1.0,
	}
	s.th = material.NewTheme()
	s.th.Shaper = s.sh
	for _, b := range builtinList {
		s.builtins[b] = true
	}
	s.tree = newTree(s.openFile)
	for c := range s.cols {
		s.cols[c] = newColumn()
	}
	return s
}

func (s *gioUI) setRoot(path string) {
	s.root = path
	s.tree.setRoot(path)
}

func (s *gioUI) relPath(path string) string {
	if s.root != "" {
		return strings.TrimPrefix(path, s.root)
	}
	return path
}

func main() {
	s := newUI()

	var start string
	if len(os.Args) > 1 {
		start = os.Args[1]
	} else if info, err := os.Stat("download-src"); err == nil && info.IsDir() {
		start, _ = filepath.Abs("download-src")
	}
	if start != "" {
		if info, err := os.Stat(start); err == nil {
			abs, _ := filepath.Abs(start)
			if info.IsDir() {
				s.setRoot(abs)
			} else {
				s.setRoot(filepath.Dir(abs))
				s.openFile(abs)
			}
		}
	}

	go func() {
		w := new(app.Window)
		w.Option(app.Title("Cedar Viewers (Gio)"), app.Size(unit.Dp(1200), unit.Dp(820)))
		// Background directory reads wake the render loop when they complete.
		s.tree.invalidate = w.Invalidate
		if err := s.loop(w); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func (s *gioUI) loop(w *app.Window) error {
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

// update processes global buttons, header actions and keyboard zoom.
func (s *gioUI) update(gtx C) {
	if s.bUp.Clicked(gtx) && s.root != "" {
		if parent := filepath.Dir(s.root); parent != s.root {
			s.setRoot(parent)
		}
	}
	if s.bZoomIn.Clicked(gtx) {
		s.zoomBy(0.1)
	}
	if s.bZoomOut.Clicked(gtx) {
		s.zoomBy(-0.1)
	}
	if s.bZoomReset.Clicked(gtx) {
		s.scale = 1.0
	}
	s.processHeaderActions(gtx)
	s.processKeys(gtx)
}

func (s *gioUI) zoomBy(d float32) { s.scale = clampf(s.scale+d, 0.5, 6.0) }

// processKeys handles Cmd/Ctrl +/-/0 zoom shortcuts.
func (s *gioUI) processKeys(gtx C) {
	mods := key.ModCommand | key.ModCtrl
	filters := []event.Filter{
		key.Filter{Focus: &s.keyTag, Name: "=", Required: key.ModCommand, Optional: key.ModShift | key.ModAlt | key.ModCtrl},
		key.Filter{Focus: &s.keyTag, Name: "+", Required: key.ModCommand, Optional: key.ModShift | key.ModAlt | key.ModCtrl},
		key.Filter{Focus: &s.keyTag, Name: "-", Required: key.ModCommand, Optional: key.ModShift | key.ModAlt | key.ModCtrl},
		key.Filter{Focus: &s.keyTag, Name: "0", Required: key.ModCommand, Optional: key.ModShift | key.ModAlt | key.ModCtrl},
		key.Filter{Focus: &s.keyTag, Name: "=", Required: key.ModCtrl, Optional: key.ModShift | key.ModAlt | key.ModCommand},
		key.Filter{Focus: &s.keyTag, Name: "+", Required: key.ModCtrl, Optional: key.ModShift | key.ModAlt | key.ModCommand},
		key.Filter{Focus: &s.keyTag, Name: "-", Required: key.ModCtrl, Optional: key.ModShift | key.ModAlt | key.ModCommand},
		key.Filter{Focus: &s.keyTag, Name: "0", Required: key.ModCtrl, Optional: key.ModShift | key.ModAlt | key.ModCommand},
	}
	_ = mods
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		ke, ok := ev.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case "=", "+":
			s.zoomBy(0.1)
		case "-":
			s.zoomBy(-0.1)
		case "0":
			s.scale = 1.0
		}
	}
}

func (s *gioUI) layout(gtx C) D {
	fill(gtx, cedarWhite, gtx.Constraints.Max)

	// Register a window-wide key focus so the zoom shortcuts are delivered.
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &s.keyTag)
	area.Pop()
	if !s.focused {
		gtx.Execute(key.FocusCmd{Tag: &s.keyTag})
		s.focused = true
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(s.globalBar),
		layout.Rigid(hrule),
		layout.Flexed(1, s.workspace),
		layout.Rigid(hrule),
		layout.Rigid(s.layoutIconTray),
	)
}

// globalBar is the black system row: title, an Up button and zoom controls.
func (s *gioUI) globalBar(gtx C) D {
	h := gtx.Dp(28)
	gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarBlack, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.UniformInset(6).Layout(gtx, func(gtx C) D {
						return s.label(gtx, serifFont, font.Bold, font.Regular, 13, "Cedar  Viewers", cedarWhite, 1)
					})
				}),
				layout.Rigid(func(gtx C) D {
					return layout.UniformInset(3).Layout(gtx, func(gtx C) D { return s.flatButton(gtx, &s.bUp, "Up") })
				}),
				layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Max.X, 1)} }),
				layout.Rigid(func(gtx C) D {
					return layout.UniformInset(3).Layout(gtx, func(gtx C) D { return s.flatButton(gtx, &s.bZoomOut, "Zoom −") })
				}),
				layout.Rigid(func(gtx C) D {
					return layout.UniformInset(3).Layout(gtx, func(gtx C) D { return s.flatButton(gtx, &s.bZoomReset, "100%") })
				}),
				layout.Rigid(func(gtx C) D {
					return layout.UniformInset(3).Layout(gtx, func(gtx C) D { return s.flatButton(gtx, &s.bZoomIn, "Zoom +") })
				}),
			)
		}),
	)
}

// workspace is the file tree beside the two Cedar columns.
func (s *gioUI) workspace(gtx C) D {
	treeW := gtx.Dp(260)
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return sized(gtx, treeW, gtx.Constraints.Max.Y, func(gtx C) D { return s.layoutTree(gtx, s.tree) })
		}),
		layout.Rigid(vrule),
		layout.Flexed(1, func(gtx C) D {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return s.layoutColumn(gtx, 0) }),
				layout.Rigid(vrule),
				layout.Flexed(1, func(gtx C) D { return s.layoutColumn(gtx, 1) }),
			)
		}),
	)
}

func vrule(gtx C) D {
	sz := image.Pt(1, gtx.Constraints.Max.Y)
	fill(gtx, cedarBlack, sz)
	return D{Size: sz}
}
