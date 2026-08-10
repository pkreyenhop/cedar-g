package tioga

import (
	"os"
	"strings"
	"testing"
)

// realDoc is a genuine Cedar Tioga document (the referenced Threads study paper).
const realDoc = "../../download-src/threadsstudypaper/ThreadsStudy.tioga"

func readReal(t *testing.T) Document {
	t.Helper()
	data, err := os.ReadFile(realDoc)
	if err != nil {
		t.Skipf("sample not present: %v", err)
	}
	return Read(data, false)
}

// TestLookHelpers checks the code-letter bit layout and the universal looks.
func TestLookHelpers(t *testing.T) {
	// letter 'a' is the top bit, so 'b' is 1<<30, etc.
	var l Look = 1 << (31 - ('b' - 'a')) // bold
	l |= 1 << (31 - ('i' - 'a'))         // italic
	if !l.Bold() || !l.Italic() || l.Underline() {
		t.Fatalf("bold/italic/underline = %v/%v/%v", l.Bold(), l.Italic(), l.Underline())
	}
	if got := l.Letters(); got != "bi" {
		t.Fatalf("Letters = %q, want %q", got, "bi")
	}
	if l.Has('z') {
		t.Fatalf("unexpected look z")
	}
}

func TestSplitRuns(t *testing.T) {
	b := Look(1 << (31 - ('b' - 'a')))
	// two spans then leftover -> a trailing plain run
	got := splitRuns("HelloWorld!", []runSpan{{look: b, n: 5}, {n: 3}})
	if len(got) != 3 || got[0].Text != "Hello" || !got[0].Look.Bold() ||
		got[1].Text != "Wor" || got[1].Look != 0 || got[2].Text != "ld!" {
		t.Fatalf("splitRuns = %+v", got)
	}
	// concatenation is loss-free
	var sb strings.Builder
	for _, r := range got {
		sb.WriteString(r.Text)
	}
	if sb.String() != "HelloWorld!" {
		t.Fatalf("runs lost text: %q", sb.String())
	}
	// no spec -> single plain run; empty text -> no runs
	if r := splitRuns("abc", nil); len(r) != 1 || r[0].Text != "abc" || r[0].Look != 0 {
		t.Fatalf("plain split = %+v", r)
	}
	if r := splitRuns("", []runSpan{{n: 3}}); r != nil {
		t.Fatalf("empty split = %+v", r)
	}
}

// TestTreeIsNested verifies Phase 0: a real document decodes to a node tree that
// actually nests (not a flat list), and that every block's runs recombine to its
// text without loss.
func TestTreeIsNested(t *testing.T) {
	d := readReal(t)
	if d.Root == nil {
		t.Fatal("no tree")
	}
	nodes, maxDepth := treeStats(d.Root, 0)
	if nodes < 50 {
		t.Fatalf("expected many nodes, got %d", nodes)
	}
	if maxDepth < 2 {
		t.Fatalf("tree is flat (maxDepth=%d); nesting was lost", maxDepth)
	}
	for i, b := range d.Blocks {
		if len(b.Runs) == 0 {
			continue
		}
		var sb strings.Builder
		for _, r := range b.Runs {
			sb.WriteString(r.Text)
		}
		if sb.String() != b.Text {
			t.Fatalf("block %d runs != text:\n runs=%q\n text=%q", i, sb.String(), b.Text)
		}
	}
}

// TestLooksPreserved verifies Phase 1: the file's real character looks survive
// decoding — some runs are bold and some italic (rather than everything plain).
func TestLooksPreserved(t *testing.T) {
	d := readReal(t)
	var bold, italic int
	forEachRun(d.Root, func(r Run) {
		if r.Look.Bold() {
			bold++
		}
		if r.Look.Italic() {
			italic++
		}
	})
	if bold == 0 || italic == 0 {
		t.Fatalf("looks lost: bold=%d italic=%d", bold, italic)
	}
}

// TestLooksLandOnWholeWords is a sanity check that runs align to their text: the
// bold "Abstract." lead-in appears as a bold run whose text starts with
// "Abstract".
func TestLooksLandOnWholeWords(t *testing.T) {
	d := readReal(t)
	found := false
	forEachRun(d.Root, func(r Run) {
		if r.Look.Bold() && strings.HasPrefix(strings.TrimSpace(r.Text), "Abstract") {
			found = true
		}
	})
	if !found {
		t.Fatal(`expected a bold run beginning "Abstract"`)
	}
}

func treeStats(n *Node, depth int) (count, maxDepth int) {
	count = 1
	maxDepth = depth
	for _, c := range n.Children {
		cc, cd := treeStats(c, depth+1)
		count += cc
		if cd > maxDepth {
			maxDepth = cd
		}
	}
	return
}

func forEachRun(n *Node, fn func(Run)) {
	for _, r := range n.Runs {
		fn(r)
	}
	for _, r := range n.Comment {
		fn(r)
	}
	for _, c := range n.Children {
		forEachRun(c, fn)
	}
}
