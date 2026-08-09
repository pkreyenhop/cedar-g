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
