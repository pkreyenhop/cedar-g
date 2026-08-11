package tioga

import (
	"os"
	"path/filepath"
	"testing"
)

// treeSig renders a tree as a signature capturing structure, formats, text and
// per-run looks — everything EncodeDoc must preserve.
func treeSig(n *Node, depth int, out *[]string) {
	for _, c := range n.Children {
		line := ""
		for i := 0; i < depth; i++ {
			line += "."
		}
		line += "[" + c.Format + "]"
		for _, r := range c.Runs {
			line += "{" + r.Look.Letters() + ":" + r.Text + "}"
		}
		if len(c.Comment) > 0 {
			line += "//" + runsText(c.Comment)
		}
		for _, p := range c.Props {
			line += "<" + p.Name + "=" + p.Value + ">"
		}
		*out = append(*out, line)
		treeSig(c, depth+1, out)
	}
}

func sig(root *Node) []string {
	var out []string
	treeSig(root, 0, &out)
	return out
}

func eqSig(a, b []string) (int, bool) {
	if len(a) != len(b) {
		return -1, false
	}
	for i := range a {
		if a[i] != b[i] {
			return i, false
		}
	}
	return 0, true
}

// TestEncodeDocRoundTripSynthetic builds a tree with nesting and looks, encodes
// it, reads it back, and checks the tree is identical.
func TestEncodeDocRoundTripSynthetic(t *testing.T) {
	d := NewDoc(nil)
	title := d.InsertSibling(nil, &Node{Format: "title", Runs: []Run{
		{Text: "Hello ", Look: lookBit('b')},
		{Text: "World"},
	}})
	head := d.InsertSibling(title, NewNode("head", "Section"))
	d.InsertChild(head, &Node{Format: "body", Runs: []Run{
		{Text: "plain and "},
		{Text: "italic", Look: lookBit('i')},
	}})
	d.InsertChild(head, NewNode("body", "")) // empty leaf

	data := EncodeDoc(d.Root)
	got := Read(data, false)
	before, after := sig(d.Root), sig(got.Root)
	if i, ok := eqSig(before, after); !ok {
		t.Fatalf("round-trip differs at %d:\n before=%v\n after =%v", i, before, after)
	}
}

// TestPropsPreserved confirms Phase 6 actually captures node properties from a
// real document (the round-trip test would pass vacuously if none were read).
func TestPropsPreserved(t *testing.T) {
	doc := readReal(t)
	names := map[string]int{}
	var count func(n *Node)
	count = func(n *Node) {
		for _, p := range n.Props {
			names[p.Name]++
		}
		for _, c := range n.Children {
			count(c)
		}
	}
	count(doc.Root)
	if len(names) == 0 {
		t.Fatal("no properties captured from a real document")
	}
	t.Logf("captured %d distinct property names", len(names))
}

// TestEncodeDocRoundTripPropsSynthetic round-trips a node carrying properties.
func TestEncodeDocRoundTripPropsSynthetic(t *testing.T) {
	d := NewDoc(nil)
	n := d.InsertSibling(nil, NewNode("body", "text"))
	n.Props = []Prop{{Name: "postfix", Value: "\x01\x02\x03"}, {Name: "Mark", Value: "x"}}
	d.Root.Props = []Prop{{Name: "StyleDef", Value: "cedar"}}

	re := Read(EncodeDoc(d.Root), false)
	if i, ok := eqSig(sig(d.Root), sig(re.Root)); !ok {
		t.Fatalf("prop round-trip differs at %d", i)
	}
	if len(re.Root.Props) != 1 || re.Root.Props[0].Name != "StyleDef" {
		t.Fatalf("root prop lost: %+v", re.Root.Props)
	}
	got := re.Root.Children[0].Props
	if len(got) != 2 || got[0].Value != "\x01\x02\x03" || got[1].Name != "Mark" {
		t.Fatalf("node props lost: %+v", got)
	}
}

// TestEncodeDocRoundTripReal round-trips genuine Cedar documents: read → encode
// → read must reproduce the same tree signature.
func TestEncodeDocRoundTripReal(t *testing.T) {
	files, _ := filepath.Glob("../../download-src/cedar/release/*/*.tioga")
	files = append(files, realDoc)
	tested := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		orig := Read(data, false)
		if orig.Root == nil || len(orig.Root.Children) == 0 {
			continue
		}
		re := Read(EncodeDoc(orig.Root), false)
		before, after := sig(orig.Root), sig(re.Root)
		if i, ok := eqSig(before, after); !ok {
			b, a := "<none>", "<none>"
			if i >= 0 && i < len(before) {
				b = before[i]
			}
			if i >= 0 && i < len(after) {
				a = after[i]
			}
			t.Fatalf("%s: round-trip differs at line %d\n before=%q\n after =%q", filepath.Base(f), i, b, a)
		}
		tested++
		if tested >= 120 {
			break
		}
	}
	if tested == 0 {
		t.Skip("no sample documents present")
	}
	t.Logf("round-tripped %d real documents", tested)
}
