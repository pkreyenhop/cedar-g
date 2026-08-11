package tioga

import "strings"

// Encode serialises plain text into a Tioga container file. Paragraphs
// (separated by newlines) each become a terminal text node under an empty root
// node — the minimal well-formed structure the reader and Cedar's Tioga accept.
// It is the inverse of Read for plain documents (no looks/props are written).
func Encode(text string) []byte {
	paras := splitParagraphs(text)

	// Text section: each paragraph's Latin-1 bytes, CR-terminated (Tioga uses CR
	// as the node separator; the reader consumes one extra byte per rope).
	var textSec []byte
	lengths := make([]int, len(paras))
	for i, p := range paras {
		b := toLatin1(p)
		lengths[i] = len(b)
		textSec = append(textSec, b...)
		textSec = append(textSec, '\r')
	}

	// Control section: root branch node, then one text node per paragraph.
	control := []byte{opStartNodeFirst} // root branch, format ""
	for _, n := range lengths {
		control = append(control, opTerminalTextNodeFirst, opRope) // text node, format ""
		control = appendVarint(control, int64(n))
	}
	control = append(control, opEndNode) // close the root

	return assemble(textSec, nil, control)
}

// EncodeDoc serialises a full node tree — nesting, per-node formats, and per-run
// looks — into a Tioga container file. It is the inverse of Read for documents:
// re-reading its output reproduces the same tree. Every node is written as an
// explicit opStartNode…opEndNode pair (so structure is unambiguous), with an
// opRuns record preceding a node's rope whenever that text carries looks.
func EncodeDoc(root *Node) []byte {
	if root == nil {
		root = &Node{}
	}
	e := &encoder{}
	// The reader supplies its own synthetic root, so emit this root's children as
	// the top-level nodes rather than wrapping them (which would add a level).
	for _, c := range root.Children {
		e.node(c)
	}
	return assemble(e.text, e.comment, e.control)
}

// encoder accumulates the three on-disk sections while walking the tree.
type encoder struct {
	text    []byte
	comment []byte
	control []byte
}

func (e *encoder) node(n *Node) {
	e.control = append(e.control, opStartNode)
	e.control = appendStr(e.control, n.Format)

	// A node's own text: written for any node that carries text, and for leaves
	// even when empty (so blank leaf nodes keep their place). Pure container
	// nodes (children, no text) emit no rope.
	leaf := len(n.Children) == 0
	if len(n.Runs) > 0 || leaf {
		e.emitRuns(n.Runs)
		e.emitRope(n.Runs, false)
	}
	if len(n.Comment) > 0 {
		e.emitRuns(n.Comment)
		e.emitRope(n.Comment, true)
	}

	for _, c := range n.Children {
		e.node(c)
	}
	e.control = append(e.control, opEndNode)
}

// emitRuns writes an opRuns record describing the runs, but only when at least
// one run carries a look (a plain rope needs none). Run lengths are in Latin-1
// bytes, matching how the reader splits the rope.
func (e *encoder) emitRuns(runs []Run) {
	any := false
	for _, r := range runs {
		if r.Look != 0 {
			any = true
			break
		}
	}
	if !any {
		return
	}
	e.control = append(e.control, opRuns)
	e.control = appendVarint(e.control, int64(len(runs)))
	for _, r := range runs {
		e.control = append(e.control, opLooks)
		v := uint32(r.Look)
		e.control = append(e.control, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
		e.control = appendVarint(e.control, int64(len(toLatin1(r.Text))))
	}
}

// emitRope writes the rope opcode plus length, and appends the node's text (with
// the CR node separator) to the text or comment section.
func (e *encoder) emitRope(runs []Run, comment bool) {
	b := toLatin1(runsText(runs))
	op := byte(opRope)
	if comment {
		op = opComment
	}
	e.control = append(e.control, op)
	e.control = appendVarint(e.control, int64(len(b)))
	if comment {
		e.comment = append(append(e.comment, b...), '\r')
	} else {
		e.text = append(append(e.text, b...), '\r')
	}
}

// appendStr writes a Tioga control string: a one-byte length then Latin-1 bytes.
func appendStr(out []byte, s string) []byte {
	b := toLatin1(s)
	if len(b) > 255 {
		b = b[:255]
	}
	out = append(out, byte(len(b)))
	return append(out, b...)
}

// splitParagraphs normalises newlines and splits into paragraphs, dropping a
// single trailing empty paragraph produced by a trailing newline.
func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	paras := strings.Split(text, "\n")
	if len(paras) > 1 && paras[len(paras)-1] == "" {
		paras = paras[:len(paras)-1]
	}
	return paras
}

// toLatin1 encodes a UTF-8 string back to the Tioga byte encoding, inverting
// toString: "←" becomes 0xac; other runes are written as their Latin-1 byte, or
// '?' when they do not fit in a byte.
func toLatin1(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '←':
			out = append(out, 0xac)
		case r <= 0xff:
			out = append(out, byte(r))
		default:
			out = append(out, '?')
		}
	}
	return out
}

// assemble lays out the four on-disk sections and the trailer.
func assemble(text, comment, control []byte) []byte {
	textLen := len(text)
	commentLen := commentHeaderLen + len(comment)                   // 2-byte id + 4-byte len + body
	totalControlLen := controlHeaderLen + len(control) + trailerLen // header + ops + trailer
	total := textLen + commentLen + totalControlLen

	out := make([]byte, 0, total)
	out = append(out, text...)

	out = append(out, 0x00, 0x00) // comment id
	out = appendLength(out, commentLen)
	out = append(out, comment...)

	out = append(out, 0x9d, 0xca) // control id
	out = appendLength(out, totalControlLen)
	out = append(out, control...)

	out = append(out, 0x85, 0x97) // trailer id
	out = appendLength(out, 0)    // propLen (0, as in real Tioga files)
	out = appendLength(out, textLen)
	out = appendLength(out, total)
	return out
}

// appendLength writes the 4-byte middle-endian length field (inverse of
// getLength).
func appendLength(out []byte, v int) []byte {
	return append(out,
		byte((v>>8)&0xff),
		byte(v&0xff),
		byte((v>>24)&0xff),
		byte((v>>16)&0xff),
	)
}

// appendVarint writes a little-endian base-128 varint (inverse of getInt).
func appendVarint(out []byte, n int64) []byte {
	for n >= 0x80 {
		out = append(out, byte(n&0x7f)|0x80)
		n >>= 7
	}
	return append(out, byte(n&0x7f))
}
