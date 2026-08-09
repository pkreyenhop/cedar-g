package main

import (
	"image"
	"image/png"
	"os"
	"strings"
	"testing"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"gioui.org/gpu/headless"
)

func TestCapture(t *testing.T) {
	out := os.Getenv("CAP_OUT")
	if out == "" {
		t.Skip("set CAP_OUT to run capture")
	}
	sp := &spike{sh: loadShaper(), builtins: map[string]bool{}, weight: 0.5, grown: -1}
	for _, b := range builtinList {
		sp.builtins[b] = true
	}
	for _, p := range strings.Split(os.Getenv("CAP_FILES"), "|") {
		if p != "" {
			sp.viewers = append(sp.viewers, sp.newViewer(p))
		}
	}
	for len(sp.viewers) < 2 {
		sp.viewers = append(sp.viewers, &gviewer{path: "(empty)"})
	}

	const W, H = 1100, 780
	win, err := headless.NewWindow(W, H)
	if err != nil {
		t.Skip("no headless GPU: " + err.Error())
	}
	defer win.Release()

	var ops op.Ops
	var q input.Router
	for i := 0; i < 2; i++ {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Pt(W, H)),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Source:      q.Source(),
		}
		sp.update(gtx)
		sp.layout(gtx)
		q.Frame(&ops)
		if err := win.Frame(&ops); err != nil {
			t.Fatal(err)
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(out + "/gio.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
