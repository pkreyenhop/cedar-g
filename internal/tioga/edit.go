package tioga

import "strings"

// Doc is a mutable Tioga document tree for structured editing. Root is a
// synthetic container whose Children are the document's top-level nodes (the
// same shape Read produces). The editing operations mirror Tioga's structural
// commands: insert sibling/child, nest/unnest, delete, and per-node text/look/
// format changes. Nodes carry no parent pointer, so operations locate a node's
// parent by walking from Root; documents are small and edits are interactive, so
// this is not a bottleneck.
type Doc struct {
	Root *Node
}

// NewDoc wraps a node tree (e.g. Document.Root) for editing. A nil root becomes
// a fresh empty document.
func NewDoc(root *Node) *Doc {
	if root == nil {
		root = &Node{}
	}
	return &Doc{Root: root}
}

// NewNode makes a terminal text node with the given format and plain text.
func NewNode(format, text string) *Node {
	n := &Node{Format: format}
	if text != "" {
		n.Runs = []Run{{Text: text}}
	}
	return n
}

// Walk visits every real node (not the synthetic root) in document order,
// calling fn with the node and its 1-based nesting depth.
func (d *Doc) Walk(fn func(n *Node, depth int)) {
	var rec func(n *Node, depth int)
	rec = func(n *Node, depth int) {
		for _, c := range n.Children {
			fn(c, depth)
			rec(c, depth+1)
		}
	}
	rec(d.Root, 1)
}

// Nodes returns all real nodes in document order.
func (d *Doc) Nodes() []*Node {
	var out []*Node
	d.Walk(func(n *Node, _ int) { out = append(out, n) })
	return out
}

// Depth returns a node's 1-based nesting depth, or 0 if it is not in the tree.
func (d *Doc) Depth(target *Node) int {
	got := 0
	d.Walk(func(n *Node, depth int) {
		if n == target {
			got = depth
		}
	})
	return got
}

// parentOf returns a node's parent (possibly the synthetic Root) and its index
// among that parent's children, or (nil, -1) if the node is not found.
func (d *Doc) parentOf(target *Node) (*Node, int) {
	var find func(p *Node) (*Node, int)
	find = func(p *Node) (*Node, int) {
		for i, c := range p.Children {
			if c == target {
				return p, i
			}
			if gp, gi := find(c); gp != nil {
				return gp, gi
			}
		}
		return nil, -1
	}
	return find(d.Root)
}

// insertAt inserts child into parent.Children at index i (clamped).
func insertAt(parent, child *Node, i int) {
	if i < 0 {
		i = 0
	}
	if i > len(parent.Children) {
		i = len(parent.Children)
	}
	parent.Children = append(parent.Children, nil)
	copy(parent.Children[i+1:], parent.Children[i:])
	parent.Children[i] = child
}

// removeChild removes parent.Children[i].
func removeChild(parent *Node, i int) {
	parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
}

// InsertSibling inserts n immediately after `after` (a CTRL-RETURN node break).
// If after is nil, n becomes the last top-level node. Returns n.
func (d *Doc) InsertSibling(after, n *Node) *Node {
	if after == nil {
		d.Root.Children = append(d.Root.Children, n)
		return n
	}
	p, i := d.parentOf(after)
	if p == nil {
		d.Root.Children = append(d.Root.Children, n)
		return n
	}
	insertAt(p, n, i+1)
	return n
}

// InsertChild inserts n as the first child of parent (CTRL-I: insert and nest).
// If parent is nil it inserts at top level. Returns n.
func (d *Doc) InsertChild(parent, n *Node) *Node {
	if parent == nil {
		parent = d.Root
	}
	insertAt(parent, n, 0)
	return n
}

// Nest makes n a child of its immediately preceding sibling (CTRL-N), deepening
// its indent by one. It fails (returns false) when n has no preceding sibling.
func (d *Doc) Nest(n *Node) bool {
	p, i := d.parentOf(n)
	if p == nil || i == 0 {
		return false
	}
	prev := p.Children[i-1]
	removeChild(p, i)
	prev.Children = append(prev.Children, n)
	return true
}

// Unnest moves n out to become the sibling immediately after its parent
// (CTRL-SHIFT-N), reducing its indent by one. It fails when n is already a
// top-level node.
func (d *Doc) Unnest(n *Node) bool {
	p, i := d.parentOf(n)
	if p == nil || p == d.Root {
		return false
	}
	gp, gi := d.parentOf(p)
	if gp == nil {
		return false
	}
	removeChild(p, i)
	insertAt(gp, n, gi+1)
	return true
}

// Delete removes n and its subtree, returning the node that should be selected
// next (the previous sibling, else the parent, else nil).
func (d *Doc) Delete(n *Node) *Node {
	p, i := d.parentOf(n)
	if p == nil {
		return nil
	}
	removeChild(p, i)
	switch {
	case i-1 >= 0 && i-1 < len(p.Children):
		return p.Children[i-1]
	case p != d.Root:
		return p
	default:
		return nil
	}
}

// SetText replaces a node's text with a single plain run, preserving its format.
func (d *Doc) SetText(n *Node, text string) {
	if text == "" {
		n.Runs = nil
		return
	}
	n.Runs = []Run{{Text: text}}
}

// SetFormat changes a node's structural format.
func (d *Doc) SetFormat(n *Node, format string) { n.Format = format }

// ToggleLook toggles a look code letter across all of a node's runs: if every
// run already carries it, it is cleared everywhere, otherwise it is set
// everywhere. (Node-level looks; sub-selection looks are a later refinement.)
func (d *Doc) ToggleLook(n *Node, letter byte) {
	if len(n.Runs) == 0 {
		return
	}
	bit := lookBit(letter)
	if bit == 0 {
		return
	}
	all := true
	for _, r := range n.Runs {
		if r.Look&bit == 0 {
			all = false
			break
		}
	}
	for i := range n.Runs {
		if all {
			n.Runs[i].Look &^= bit
		} else {
			n.Runs[i].Look |= bit
		}
	}
}

// lookBit returns the Look bit for a code letter, or 0 if out of range.
func lookBit(letter byte) Look {
	if letter < 'a' || letter > 'a'+31 {
		return 0
	}
	return 1 << (31 - (letter - 'a'))
}

// FlattenBlocks derives the flat Block list a renderer consumes from a node
// tree, mirroring the reader: one block per node's text (and one Quote block for
// an attached comment), classified by the same blockKind rules. It lets the
// viewer refresh after structural edits without re-reading the file.
func FlattenBlocks(root *Node) []Block {
	if root == nil {
		return nil
	}
	var out []Block
	var rec func(n *Node, depth int)
	rec = func(n *Node, depth int) {
		for _, c := range n.Children {
			text := c.Text()
			if len(c.Runs) > 0 {
				kind, level := blockKind(c.Format, depth, false)
				out = append(out, Block{
					Text: text, Runs: styledRuns(c.Runs),
					Format: c.Format, Depth: depth, Kind: kind, Level: level,
				})
			}
			if len(c.Comment) > 0 {
				ctext := runsText(c.Comment)
				out = append(out, Block{
					Text: ctext, Runs: styledRuns(c.Comment),
					Format: c.Format, Depth: depth, Kind: Quote,
				})
			}
			rec(c, depth+1)
		}
	}
	rec(root, 1)
	return out
}

func runsText(runs []Run) string {
	var sb strings.Builder
	for _, r := range runs {
		sb.WriteString(r.Text)
	}
	return sb.String()
}
