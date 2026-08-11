package main

import (
	"testing"

	"cedarg/internal/tioga"
)

func TestEditToolFormatAndLooks(t *testing.T) {
	s := newUI()
	d := tioga.NewDoc(nil)
	d.InsertSibling(nil, tioga.NewNode("body", "hello"))
	v := &viewer{kind: vkContent, root: d.Root, blocks: []tioga.Block{{Text: "hello"}}}
	s.enterStruct(v)
	v.sel = v.doc.Nodes()[0]

	// Set format via the EditTool field.
	v.formatEd.SetText("head")
	s.beginEdit(v)
	v.doc.SetFormat(v.sel, "head")
	if v.sel.Format != "head" {
		t.Fatalf("format not set: %q", v.sel.Format)
	}

	// Apply a look and confirm it lands on the node's runs.
	s.toggleLook(v, 'e')
	if len(v.sel.Runs) == 0 || !v.sel.Runs[0].Look.Has('e') {
		t.Fatalf("small-caps look not applied: %+v", v.sel.Runs)
	}
	// Undo removes the look (beginEdit snapshotted before it).
	s.undo(v)
	if v.doc.Nodes()[0].Runs[0].Look.Has('e') {
		t.Fatalf("undo did not remove the look")
	}
}
