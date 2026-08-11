package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cedarg/internal/tioga"
)

// TestStructEditFlow drives the structure editor end to end without the GUI
// event loop: enter, edit a node's text, apply a look, run a structural op, then
// exit and confirm the rebuilt block view reflects every change.
func TestStructEditFlow(t *testing.T) {
	d := tioga.NewDoc(nil)
	a := d.InsertSibling(nil, tioga.NewNode("head", "A"))
	d.InsertChild(a, tioga.NewNode("body", "body1"))
	d.InsertSibling(a, tioga.NewNode("head", "B"))

	v := &viewer{kind: vkContent, root: d.Root, blocks: []tioga.Block{{Text: "A"}}}
	s := newUI()

	s.enterStruct(v)
	if !v.structEdit || v.doc == nil || v.sel == nil {
		t.Fatal("enterStruct did not initialise editing state")
	}

	// Edit the first node's text through its editor, then sync into the tree.
	first := v.doc.Nodes()[0]
	v.editorFor(first).SetText("A-edited")
	s.syncEditors(v)
	if first.Text() != "A-edited" {
		t.Fatalf("syncEditors did not write text: %q", first.Text())
	}

	// Bold the selected node.
	v.sel = first
	v.doc.ToggleLook(first, 'b')

	// Nest B (the third node) under the edited first node.
	nodes := v.doc.Nodes()
	if !v.doc.Nest(nodes[len(nodes)-1]) {
		t.Fatal("nest failed")
	}

	// Exit rebuilds the read-only blocks from the mutated tree.
	s.exitStruct(v)
	if v.structEdit {
		t.Fatal("exitStruct left editing on")
	}
	if len(v.blocks) == 0 || v.blocks[0].Text != "A-edited" {
		t.Fatalf("blocks not rebuilt from edits: %+v", v.blocks)
	}
	if len(v.blocks[0].Runs) == 0 || !v.blocks[0].Runs[0].Look.Bold() {
		t.Fatalf("bold look lost on rebuild: %+v", v.blocks[0])
	}
	// The nested B should now be deeper than a top-level node.
	var maxDepth int
	for _, b := range v.blocks {
		if b.Depth > maxDepth {
			maxDepth = b.Depth
		}
	}
	if maxDepth < 2 {
		t.Fatalf("nesting not reflected in rebuilt blocks (maxDepth=%d)", maxDepth)
	}
}

// TestStructSaveReopen edits a document in the structure editor, saves it as a
// real Tioga file, and reopens it — the edits (text + bold look + nesting) must
// survive the on-disk round trip.
func TestStructSaveReopen(t *testing.T) {
	dir := t.TempDir()
	// Seed a real Tioga file on disk via EncodeDoc of a small tree.
	d0 := tioga.NewDoc(nil)
	a := d0.InsertSibling(nil, tioga.NewNode("head", "Alpha"))
	d0.InsertChild(a, tioga.NewNode("body", "child"))
	path := filepath.Join(dir, "Doc.tioga")
	if err := os.WriteFile(path, tioga.EncodeDoc(d0.Root), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newUI()
	s.setRoot(dir)
	v := s.newViewer(path)
	if !v.isDoc() {
		t.Fatalf("expected a document viewer, got kind=%d isCode=%v", v.kind, v.isCode)
	}
	s.enterStruct(v)
	first := v.doc.Nodes()[0]
	v.editorFor(first).SetText("Alpha-2")
	s.syncEditors(v)
	v.sel = first
	v.doc.ToggleLook(first, 'b')
	s.saveStruct(v)
	if !strings.HasPrefix(v.saveMsg, "saved") {
		t.Fatalf("save failed: %q", v.saveMsg)
	}

	// Reopen from disk and confirm the edits persisted.
	re := tioga.Read(mustRead(t, path), false)
	got := re.Root.Children[0]
	if got.Text() != "Alpha-2" {
		t.Fatalf("text not persisted: %q", got.Text())
	}
	if len(got.Runs) == 0 || !got.Runs[0].Look.Bold() {
		t.Fatalf("bold look not persisted: %+v", got.Runs)
	}
	if len(got.Children) != 1 || got.Children[0].Text() != "child" {
		t.Fatalf("nesting not persisted: %+v", got.Children)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
