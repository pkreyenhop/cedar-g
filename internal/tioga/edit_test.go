package tioga

import (
	"strings"
	"testing"
)

// outline renders the tree as an indented text outline, for asserting structure.
func (d *Doc) outline() string {
	var sb strings.Builder
	d.Walk(func(n *Node, depth int) {
		sb.WriteString(strings.Repeat("  ", depth-1))
		sb.WriteString(n.Text())
		sb.WriteByte('\n')
	})
	return sb.String()
}

// build makes a small document: A, B (child B1), C.
func build() *Doc {
	d := NewDoc(nil)
	a := d.InsertSibling(nil, NewNode("head", "A"))
	_ = a
	b := d.InsertSibling(a, NewNode("head", "B"))
	d.InsertChild(b, NewNode("body", "B1"))
	d.InsertSibling(b, NewNode("head", "C"))
	return d
}

func TestBuildAndWalk(t *testing.T) {
	got := build().outline()
	want := "A\nB\n  B1\nC\n"
	if got != want {
		t.Fatalf("outline =\n%q\nwant\n%q", got, want)
	}
}

func TestInsertSiblingAndChild(t *testing.T) {
	d := NewDoc(nil)
	a := d.InsertSibling(nil, NewNode("", "A"))
	c := d.InsertSibling(a, NewNode("", "C"))
	d.InsertSibling(a, NewNode("", "B")) // between A and C
	if got := d.outline(); got != "A\nB\nC\n" {
		t.Fatalf("sibling order = %q", got)
	}
	d.InsertChild(c, NewNode("", "C0"))
	d.InsertChild(c, NewNode("", "C-first")) // first child goes on top
	if got := d.outline(); got != "A\nB\nC\n  C-first\n  C0\n" {
		t.Fatalf("child insert = %q", got)
	}
}

func TestNest(t *testing.T) {
	d := build()
	nodes := d.Nodes() // A,B,B1,C
	c := nodes[3]
	if !d.Nest(c) { // C nests under B
		t.Fatal("nest C failed")
	}
	if got := d.outline(); got != "A\nB\n  B1\n  C\n" {
		t.Fatalf("after nest =\n%q", got)
	}
	// A has no preceding sibling -> cannot nest.
	if d.Nest(nodes[0]) {
		t.Fatal("nesting first node should fail")
	}
}

func TestUnnest(t *testing.T) {
	d := build()
	b1 := d.Nodes()[2] // B1, child of B
	if !d.Unnest(b1) { // B1 moves out to sibling after B
		t.Fatal("unnest B1 failed")
	}
	if got := d.outline(); got != "A\nB\nB1\nC\n" {
		t.Fatalf("after unnest =\n%q", got)
	}
	// A top-level node cannot unnest further.
	if d.Unnest(d.Nodes()[0]) {
		t.Fatal("unnesting top-level node should fail")
	}
}

func TestNestUnnestRoundTrip(t *testing.T) {
	d := build()
	before := d.outline()
	c := d.Nodes()[3]
	if !d.Nest(c) {
		t.Fatal("nest failed")
	}
	if !d.Unnest(c) {
		t.Fatal("unnest failed")
	}
	if got := d.outline(); got != before {
		t.Fatalf("nest+unnest changed tree:\n%q\nvs\n%q", got, before)
	}
}

func TestDelete(t *testing.T) {
	d := build()
	b := d.Nodes()[1] // B (with child B1)
	sel := d.Delete(b)
	if got := d.outline(); got != "A\nC\n" {
		t.Fatalf("after delete B =\n%q", got)
	}
	if sel == nil || sel.Text() != "A" { // previous sibling selected
		t.Fatalf("delete should select previous sibling A, got %v", sel)
	}
}

func TestSetTextAndFormat(t *testing.T) {
	d := build()
	a := d.Nodes()[0]
	d.SetText(a, "A-edited")
	d.SetFormat(a, "title")
	if a.Text() != "A-edited" || a.Format != "title" {
		t.Fatalf("set text/format: %q / %q", a.Text(), a.Format)
	}
	d.SetText(a, "")
	if len(a.Runs) != 0 {
		t.Fatalf("empty text should clear runs")
	}
}

func TestToggleLook(t *testing.T) {
	d := NewDoc(nil)
	n := d.InsertSibling(nil, NewNode("", "hello"))
	d.ToggleLook(n, 'b') // set bold everywhere
	if !n.Runs[0].Look.Bold() {
		t.Fatal("bold not set")
	}
	d.ToggleLook(n, 'b') // clear
	if n.Runs[0].Look.Bold() {
		t.Fatal("bold not cleared")
	}
	// Mixed -> becomes all set.
	n.Runs = []Run{{Text: "a", Look: lookBit('b')}, {Text: "b"}}
	d.ToggleLook(n, 'b')
	if !n.Runs[0].Look.Bold() || !n.Runs[1].Look.Bold() {
		t.Fatal("mixed toggle should set all")
	}
}

// TestEditRealDocument runs the operations against a genuine parsed document to
// ensure parent-lookup and mutation work on real (deep) trees.
func TestEditRealDocument(t *testing.T) {
	doc := readReal(t)
	d := NewDoc(doc.Root)
	before := len(d.Nodes())
	first := d.Nodes()[0]
	d.InsertSibling(first, NewNode("body", "inserted"))
	if len(d.Nodes()) != before+1 {
		t.Fatalf("insert did not grow tree")
	}
	n := d.Nodes()[1]
	depthBefore := d.Depth(n)
	if d.Nest(n) && d.Depth(n) != depthBefore+1 {
		t.Fatalf("nest did not deepen node")
	}
}

func TestFlattenBlocksBasic(t *testing.T) {
	d := build() // A, B(child B1), C  — all heads except body B1
	d.SetFormat(d.Nodes()[0], "title")
	bs := FlattenBlocks(d.Root)
	if len(bs) != 4 {
		t.Fatalf("want 4 blocks, got %d", len(bs))
	}
	if bs[0].Text != "A" || bs[0].Format != "title" {
		t.Fatalf("block0 = %+v", bs[0])
	}
	if bs[2].Text != "B1" || bs[2].Depth != 2 {
		t.Fatalf("B1 should be depth 2: %+v", bs[2])
	}
}
