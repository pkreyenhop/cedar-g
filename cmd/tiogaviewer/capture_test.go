package main

import (
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

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
			s.tree.toggle(kids[0].path)
		}
	}
	if os.Getenv("CAP_MIN") != "" && len(s.cols[0].viewers) > 0 {
		s.minimizeViewer(s.cols[0].viewers[0])
	}
	if p := os.Getenv("CAP_TEXT"); p != "" {
		s.openFile(p)
	}
	if lv := os.Getenv("CAP_LEVELS"); lv != "" {
		for _, c := range s.cols {
			for _, v := range c.viewers {
				if v.isDoc() {
					v.showLevels = true
					if n, err := strconv.Atoi(lv); err == nil {
						v.levelCap = n
					}
				}
			}
		}
	}
	if os.Getenv("CAP_NEW") != "" {
		s.openNewDocument()
		for _, c := range s.cols {
			for _, v := range c.viewers {
				if v.kind == vkEditor {
					v.editor.SetText("Weiser, August 4, 1993\n\nUsing Threads in Interactive Systems: A Case Study\n\nWe describe the results of examining two large systems.")
				}
			}
		}
	}
	if os.Getenv("CAP_RUN") != "" {
		s.openNewDocument()
		var ed *viewer
		for _, c := range s.cols {
			for _, v := range c.viewers {
				if v.kind == vkEditor {
					ed = v
				}
			}
		}
		if ed != nil {
			ed.nameEd.SetText("Factorial.mesa")
			ed.editor.SetText("Factorial: PROGRAM =\nBEGIN\n  Fact: PROCEDURE [n: INTEGER] RETURNS [INTEGER] =\n    BEGIN\n      IF n <= 1 THEN RETURN [1];\n      RETURN [n * Fact[n - 1]];\n    END;\n  i: INTEGER;\n  FOR i IN [1..8] DO\n    IO.PutF[\"%g! = %g\\n\", i, Fact[i]];\n  ENDLOOP;\nEND.")
			s.runViewer(ed)
		}
	}
	if os.Getenv("CAP_TERM") != "" {
		s.openTerminal()
		for _, c := range s.cols {
			for _, v := range c.viewers {
				if v.term != nil {
					v.term.write("ls -la | head\r")
				}
			}
		}
		time.Sleep(700 * time.Millisecond) // let the shell produce output
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
