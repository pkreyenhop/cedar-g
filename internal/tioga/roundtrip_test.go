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
		if tested >= 40 {
			break
		}
	}
	if tested == 0 {
		t.Skip("no sample documents present")
	}
	t.Logf("round-tripped %d real documents", tested)
}
