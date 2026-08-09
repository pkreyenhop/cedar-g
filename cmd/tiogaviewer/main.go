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

	root       string
	tree       *tree
	showTree   bool
	invalidate func() // wakes the render loop (background readers, terminals)

	cols      [numColumns]*column
	minimized []*viewer

	bUp, bCmd, bOpen, bNew widget.Clickable

	treeWidth float32 // tree column width, in dp
	colSplit  float32 // column 0's fraction of the columns area
	treeDrag  bool
	colDrag   bool

	keyTag  int
	focused bool
}

func newUI() *gioUI {
	s := &gioUI{
		sh:        loadShaper(),
		builtins:  map[string]bool{},
		scale:     1.0,
		treeWidth: 240,
		colSplit:  0.5,
		showTree:  true,
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
		w.Option(app.Title("Cedar Viewers (Gio)"), app.Size(unit.Dp(1200), unit.Dp(820)), app.Fullscreen.Option())
		// Background directory reads / terminal output wake the render loop.
		s.invalidate = w.Invalidate
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
	if s.bCmd.Clicked(gtx) {
		s.openTerminal()
	}
	if s.bOpen.Clicked(gtx) {
		s.showTree = !s.showTree // toggle the file selector
	}
	if s.bNew.Clicked(gtx) {
		s.openNewDocument()
	}
	s.processHeaderActions(gtx)
	s.processKeys(gtx)
}

func (s *gioUI) zoomBy(d float32) { s.scale = clampf(s.scale+d, 0.5, 6.0) }

// processKeys handles the Cmd +/=/- /0 zoom shortcuts.
func (s *gioUI) processKeys(gtx C) {
	names := []key.Name{"=", "+", "-", "0"}
	filters := make([]event.Filter, len(names))
	for i, nm := range names {
		filters[i] = key.Filter{Focus: &s.keyTag, Name: nm, Required: key.ModCommand, Optional: key.ModShift}
	}
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
					return layout.UniformInset(3).Layout(gtx, func(gtx C) D { return s.flatButton(gtx, &s.bCmd, "Cmd") })
				}),
				layout.Rigid(func(gtx C) D {
					return layout.UniformInset(3).Layout(gtx, func(gtx C) D { return s.flatButton(gtx, &s.bOpen, "Open") })
				}),
				layout.Rigid(func(gtx C) D {
					return layout.UniformInset(3).Layout(gtx, func(gtx C) D { return s.flatButton(gtx, &s.bNew, "New") })
				}),
			)
		}),
	)
}

// workspace is the file tree beside the two Cedar columns, with draggable
// vertical dividers between the tree and the columns, and between the columns.
func (s *gioUI) workspace(gtx C) D {
	cols := func(gtx C) D {
		return s.hsplit(gtx, &s.colSplit, &s.colDrag,
			func(gtx C) D { return s.layoutColumn(gtx, 0) },
			func(gtx C) D { return s.layoutColumn(gtx, 1) },
		)
	}
	if !s.showTree {
		return cols(gtx)
	}
	// File selector on the left, columns fill the rest (both draggable).
	return s.hsplitW(gtx, &s.treeWidth, &s.treeDrag,
		func(gtx C) D { return s.layoutTree(gtx, s.tree) },
		cols,
	)
}
