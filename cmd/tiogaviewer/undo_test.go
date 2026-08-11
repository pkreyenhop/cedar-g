package main

import (
	"testing"

	"cedarg/internal/tioga"
)

func TestUndoRedo(t *testing.T) {
	s := newUI()
	d := tioga.NewDoc(nil)
	d.InsertSibling(nil, tioga.NewNode("head", "A"))
	v := &viewer{kind: vkContent, root: d.Root, blocks: []tioga.Block{{Text: "A"}}}
	s.enterStruct(v)
	v.sel = v.doc.Nodes()[0]

	// A structural edit: add a child.
	s.beginEdit(v)
	v.doc.InsertChild(v.sel, tioga.NewNode("body", "child"))
	if len(v.doc.Nodes()) != 2 {
		t.Fatalf("expected 2 nodes after insert, got %d", len(v.doc.Nodes()))
	}

	s.undo(v)
	if len(v.doc.Nodes()) != 1 {
		t.Fatalf("undo did not remove the child: %d nodes", len(v.doc.Nodes()))
	}
	if v.doc.Nodes()[0].Text() != "A" {
		t.Fatalf("undo lost the original node: %q", v.doc.Nodes()[0].Text())
	}

	s.redo(v)
	if len(v.doc.Nodes()) != 2 {
		t.Fatalf("redo did not restore the child: %d nodes", len(v.doc.Nodes()))
	}

	// A new edit clears the redo stack.
	s.beginEdit(v)
	v.doc.InsertSibling(v.doc.Nodes()[0], tioga.NewNode("head", "B"))
	if len(v.redoStack) != 0 {
		t.Fatalf("new edit should clear redo, have %d", len(v.redoStack))
	}

	// Snapshots are deep clones: mutating the live tree must not change a snapshot.
	s.beginEdit(v)
	snap := v.undoStack[len(v.undoStack)-1]
	snapCount := countNodes(snap)
	v.doc.InsertChild(v.doc.Nodes()[0], tioga.NewNode("body", "more"))
	if countNodes(snap) != snapCount {
		t.Fatalf("live edit leaked into the undo snapshot")
	}
}

func countNodes(n *tioga.Node) int {
	c := 0
	for _, ch := range n.Children {
		c += 1 + countNodes(ch)
	}
	return c
}
