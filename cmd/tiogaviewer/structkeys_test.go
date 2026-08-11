package main

import (
	"image"
	"testing"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"cedarg/internal/tioga"
)

// TestStructKeyGestures proves the CTRL structural keys fire through the focused
// node editor: Ctrl+Return adds a sibling, Ctrl+N nests the selection.
func TestStructKeyGestures(t *testing.T) {
	s := newUI()
	d := tioga.NewDoc(nil)
	d.InsertSibling(nil, tioga.NewNode("head", "A"))
	d.InsertSibling(nil, tioga.NewNode("head", "B"))
	v := &viewer{kind: vkContent, root: d.Root, blocks: []tioga.Block{{Text: "A"}}}
	s.enterStruct(v)
	v.sel = v.doc.Nodes()[0]
	v.focusNode = v.sel

	var ops op.Ops
	var q input.Router
	frame := func() {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Pt(500, 300)),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Source:      q.Source(),
		}
		s.processStruct(gtx, v)
		s.structBody(gtx, v)
		q.Frame(&ops)
	}
	frame()
	frame() // let focus settle on the selected node's editor

	before := len(v.doc.Nodes())
	q.Queue(key.Event{Name: key.NameReturn, Modifiers: key.ModCtrl, State: key.Press})
	frame()
	if got := len(v.doc.Nodes()); got != before+1 {
		t.Fatalf("Ctrl+Return did not add a sibling: %d -> %d", before, got)
	}

	// Ctrl+N nests the (newly selected) node under its previous sibling.
	target := v.sel
	depthBefore := v.doc.Depth(target)
	frame() // ensure the new node's editor is focused
	q.Queue(key.Event{Name: "N", Modifiers: key.ModCtrl, State: key.Press})
	frame()
	if d := v.doc.Depth(target); d <= depthBefore {
		t.Fatalf("Ctrl+N did not nest (depth %d -> %d)", depthBefore, d)
	}
}
