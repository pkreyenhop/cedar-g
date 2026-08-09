package main

import (
	"image"
	"image/png"
	"os"
	"strings"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func TestCapture(t *testing.T) {
	out := os.Getenv("CAP_OUT")
	if out == "" {
		t.Skip("set CAP_OUT to run capture")
	}
	s := newUI()
	if r := os.Getenv("CAP_ROOT"); r != "" {
		s.setRoot(r)
	}
	for _, p := range strings.Split(os.Getenv("CAP_FILES"), "|") {
		if p != "" {
			s.openFile(p)
		}
	}
	if os.Getenv("CAP_EXPAND") != "" && s.root != "" {
		if kids := s.tree.children(s.root); len(kids) > 0 {
			s.tree.expanded[kids[0]] = true
		}
	}
	if os.Getenv("CAP_MIN") != "" && len(s.cols[0].viewers) > 0 {
		s.minimizeViewer(s.cols[0].viewers[0])
	}

	const W, H = 1200, 820
	win, err := headless.NewWindow(W, H)
	if err != nil {
		t.Skip("no headless GPU: " + err.Error())
	}
	defer win.Release()

	var ops op.Ops
	var q input.Router
	for i := 0; i < 3; i++ {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Pt(W, H)),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Source:      q.Source(),
		}
		s.update(gtx)
		s.layout(gtx)
		q.Frame(&ops)
		if err := win.Frame(&ops); err != nil {
			t.Fatal(err)
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(out + "/gio_full.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
