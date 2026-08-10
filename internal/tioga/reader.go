// Package tioga decodes the Xerox Tioga editor file format used throughout the
// Cedar/Mesa sources. It is a Go port of TiogaReader.cpp from Rochus Keller's
// TiogaViewer. Unlike the original, which emitted HTML, this reader produces a
// small toolkit-agnostic block model for documents (and indented plain text for
// code), so a native GUI can render it directly.
package tioga

import (
	"strings"
	"unicode/utf8"
)

// decodeFallback decodes a file that is not in the Tioga container format. If the
// bytes are already valid UTF-8 (e.g. a modern source that literally contains
// "←"), they are kept as text; otherwise they are treated as Latin-1 via
// toString. In both cases Cedar's "_" assignment alias is displayed as "←", and
// classic CR / CRLF line endings are normalised to "\n" so line structure (and
// comment termination) survives.
func decodeFallback(data []byte) string {
	var s string
	if utf8.Valid(data) {
		s = strings.ReplaceAll(string(data), "_", "←")
	} else {
		s = toString(data)
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// BlockKind classifies a rendered document block.
type BlockKind int

const (
	Paragraph BlockKind = iota
	Heading
	Code
	Quote
)

// Block is one rendered piece of a Tioga document.
type Block struct {
	Kind  BlockKind
	Level int // heading level 1..6 (Heading only)
	Text  string
}

// Document is the decoded result. When IsCode is true, Code holds the
// reconstructed, indented source; otherwise Blocks holds the document body.
type Document struct {
	IsCode bool
	Code   string
	Blocks []Block
}

// Format-table sizing, matching the original constants.
const (
	numFormats       = 70
	numLooks         = 50
	numProps         = 50
	lenLen           = 4
	idLen            = 2
	commentHeaderLen = idLen + lenLen
	controlHeaderLen = idLen + lenLen
	trailerLen       = idLen + 3*lenLen
)

// Control opcodes. Ranges (…First/…Last) encode a format or looks index in the
// opcode byte itself; see the original for the on-disk format description.
const (
	opEndOfFile = 0
	opStartNode = 1

	opStartNodeFirst = 2
	opStartNodeLast  = opStartNodeFirst + numFormats // 72

	opTerminalTextNode      = 73
	opTerminalTextNodeFirst = 74
	opTerminalTextNodeLast  = opTerminalTextNodeFirst + numFormats // 144

	opOtherNode           = 145
	opOtherNodeShort      = 146
	opOtherNodeSpecs      = 147
	opOtherNodeSpecsShort = 148
	opProp                = 149
	opPropShort           = 150
	opEndNode             = 151
	opRope                = 152
	opComment             = 153
	opRuns                = 154
	opLooks               = 155

	opLooksFirst = 156
	opLooksLast  = opLooksFirst + numLooks // 206

	opLook1 = 207
	opLook2 = 208
	opLook3 = 209
)

// stream is a cursor into the shared buffer, delimited by [next, limit).
type stream struct {
	next  int
	limit int
}

type reader struct {
	buf    []byte
	isCode bool

	text stream
	com  stream
	ctrl stream

	str []byte // scratch for the most recent rope/name

	nFormats int
	formats  [numFormats]string

	nLooks int
	looks  [numLooks]int64

	nProps int
	props  [numProps]string

	level []string // stack of node format names (indent level)

	// output
	code   strings.Builder
	blocks []Block
}

// Read decodes data. If the buffer is not a valid Tioga file it is treated as
// plain text. isCode selects code reconstruction over document rendering.
func Read(data []byte, isCode bool) Document {
	r := &reader{isCode: isCode}
	if r.init(data) {
		r.doWork()
	} else {
		// Not a Tioga container: fall back to the decoded raw bytes.
		txt := decodeFallback(data)
		if isCode {
			return Document{IsCode: true, Code: txt}
		}
		return Document{Blocks: []Block{{Kind: Paragraph, Text: txt}}}
	}
	if isCode {
		return Document{IsCode: true, Code: r.code.String()}
	}
	return Document{Blocks: r.blocks}
}

func checkID(buf []byte, p int, id []byte) bool {
	if p+len(id) > len(buf) {
		return false
	}
	for i := 0; i < len(id); i++ {
		if buf[p+i] != id[i] {
			return false
		}
	}
	return true
}

// getLength reads the 4-byte middle-endian length field the format uses.
func getLength(buf []byte, p int) int {
	var result int
	result |= int(buf[p]) << 8
	result |= int(buf[p+1])
	result |= int(buf[p+2]) << 24
	result |= int(buf[p+3]) << 16
	return result
}

func (r *reader) init(buf []byte) bool {
	length := len(buf)
	if length < trailerLen {
		return false
	}
	trailerID := []byte{0x85, 0x97}
	commentID := []byte{0, 0}
	controlID := []byte{0x9d, 0xca}

	p := length - trailerLen
	if !checkID(buf, p, trailerID) {
		return false
	}
	p += idLen
	propLen := getLength(buf, p)
	p += lenLen
	textLen := getLength(buf, p)
	p += lenLen
	totalSize := getLength(buf, p)

	if totalSize != length || propLen > length || textLen > length || textLen < 0 {
		return false
	}
	if textLen+idLen+lenLen > length {
		return false
	}
	if !checkID(buf, textLen, commentID) {
		return false
	}
	commentLen := getLength(buf, textLen+idLen)
	if commentLen < 0 || textLen+commentLen+idLen+lenLen > length {
		return false
	}
	if !checkID(buf, textLen+commentLen, controlID) {
		return false
	}
	totalControlLen := getLength(buf, textLen+commentLen+idLen)
	if length != textLen+commentLen+totalControlLen {
		return false
	}

	r.buf = buf
	r.text = stream{next: 0, limit: textLen}
	r.com = stream{next: textLen + commentHeaderLen, limit: textLen + commentLen}
	r.ctrl = stream{next: textLen + commentLen + controlHeaderLen, limit: length - trailerLen}

	r.str = nil
	r.nFormats = 1
	r.formats[0] = ""
	r.nLooks = 1
	r.looks[0] = 0
	r.nProps = 1
	r.props[0] = ""
	r.addProp("prefix")
	r.addProp("postfix")
	return true
}

func (r *reader) doWork() {
	terminalNode := false
	lastWasTerminal := false
	level := 0
	var iFormat int
	var runLen int64

	op := r.getOp()
	for {
		switch {
		case op >= opTerminalTextNodeFirst && op <= opTerminalTextNodeLast:
			iFormat = op - opTerminalTextNodeFirst
			if iFormat >= r.nFormats {
				iFormat = 0
			}
			terminalNode = true
		case op >= opStartNodeFirst && op <= opStartNodeLast:
			iFormat = op - opStartNodeFirst
			if iFormat >= r.nFormats {
				iFormat = 0
			}
			terminalNode = false
		default:
			switch op {
			case opEndNode:
				if level >= 0 {
					level--
				}
				r.endNode()
				op = r.getOp()
				continue
			case opStartNode:
				r.getStr()
				iFormat = r.addFormat(string(r.str))
				terminalNode = false
			case opTerminalTextNode:
				r.getStr()
				iFormat = r.addFormat(string(r.str))
				terminalNode = true
			case opRope, opComment:
				length := r.getInt()
				if op == opRope {
					r.sGetRope(&r.text, length+1)
				} else {
					r.sGetRope(&r.com, length+1)
				}
				fixNewlines(r.str)
				r.insertText(r.str, length, op == opComment)
				runLen = 0
				op = r.getOp()
				continue
			case opRuns:
				nRuns := r.getInt()
				runLen = 0
				for i := int64(0); i < nRuns; i++ {
					iLook := 0
					op = r.getOp()
					switch {
					case op >= opLooksFirst && op <= opLooksLast:
						iLook = op - opLooksFirst
					case op >= opLook1 && op <= opLook3:
						iLook = r.getLookChars(op - opLook1 + 1)
					case op == opLooks:
						l := int64(r.getByte())
						l = (l << 8) | int64(r.getByte())
						l = (l << 8) | int64(r.getByte())
						l = (l << 8) | int64(r.getByte())
						iLook = r.addLooks(l)
					}
					if iLook >= r.nLooks {
						iLook = 0
					}
					rl := r.getInt()
					runLen += rl
				}
				op = r.getOp()
				continue
			case opProp:
				r.getStr()
				r.addProp(string(r.str))
				n := r.getInt()
				r.sGetRope(&r.ctrl, n)
				op = r.getOp()
				continue
			case opPropShort:
				iProp := r.getByte()
				if iProp >= r.nProps {
					iProp = 0
				}
				_ = iProp
				n := r.getInt()
				r.sGetRope(&r.ctrl, n)
				op = r.getOp()
				continue
			case opEndOfFile:
				if level == 0 {
					// top level: fall through to loop-exit check below
				} else {
					op = opEndNode
					continue
				}
			default:
				// otherNode variants and unknown ops are skipped.
				op = r.getOp()
				continue
			}
		}

		if level == 0 && op == opEndOfFile {
			break
		}
		if lastWasTerminal {
			r.endNode()
		}
		lastWasTerminal = terminalNode
		r.startNode(r.formats[iFormat])
		if !terminalNode {
			level++
		}
		op = r.getOp()
	}
}

func (r *reader) getOp() int {
	if r.ctrl.next < r.ctrl.limit {
		v := int(r.buf[r.ctrl.next])
		r.ctrl.next++
		return v
	}
	return opEndOfFile
}

func (r *reader) getByte() int {
	if r.ctrl.next < r.ctrl.limit {
		v := int(r.buf[r.ctrl.next])
		r.ctrl.next++
		return v
	}
	return 0
}

// getInt reads a little-endian base-128 varint from the control stream.
func (r *reader) getInt() int64 {
	var result int64
	nBits := uint(0)
	for r.ctrl.next < r.ctrl.limit && r.buf[r.ctrl.next]&0x80 != 0 {
		result |= int64(r.buf[r.ctrl.next]&0x7f) << nBits
		r.ctrl.next++
		nBits += 7
	}
	if r.ctrl.next < r.ctrl.limit {
		result |= int64(r.buf[r.ctrl.next]&0x7f) << nBits
		r.ctrl.next++
	}
	return result
}

func (r *reader) getStr() {
	if r.ctrl.next >= r.ctrl.limit {
		r.str = nil
		return
	}
	n := int64(r.buf[r.ctrl.next])
	r.ctrl.next++
	r.sGetRope(&r.ctrl, n)
}

// sGetRope copies n bytes out of s into the scratch buffer.
func (r *reader) sGetRope(s *stream, n int64) {
	if n < 0 {
		n = 0
	}
	out := make([]byte, 0, n)
	for i := int64(0); i < n; i++ {
		if s.next >= s.limit || s.next >= len(r.buf) {
			break
		}
		out = append(out, r.buf[s.next])
		s.next++
	}
	r.str = out
}

func fixNewlines(s []byte) {
	for i := range s {
		if s[i] == '\r' {
			s[i] = '\n'
		}
	}
}

func (r *reader) getLookChars(n int) int {
	var l int64
	for n > 0 {
		c := r.getByte()
		if 'a' <= c && c <= 'a'+31 {
			l |= 1 << uint(31-(c-'a'))
		}
		n--
	}
	return r.addLooks(l)
}

func (r *reader) addFormat(format string) int {
	if format == "" {
		return 0
	}
	for i := 1; i < r.nFormats; i++ {
		if r.formats[i] == format {
			return i
		}
	}
	if r.nFormats < numFormats {
		r.formats[r.nFormats] = format
		r.nFormats++
		return r.nFormats - 1
	}
	return 0
}

func (r *reader) addLooks(l int64) int {
	if l == 0 {
		return 0
	}
	for i := 1; i < r.nLooks; i++ {
		if r.looks[i] == l {
			return i
		}
	}
	if r.nLooks < numLooks {
		r.looks[r.nLooks] = l
		r.nLooks++
		return r.nLooks - 1
	}
	return 0
}

func (r *reader) addProp(name string) int {
	if name == "" {
		return 0
	}
	for i := 1; i < r.nProps; i++ {
		if r.props[i] == name {
			return i
		}
	}
	if r.nProps < numProps {
		r.props[r.nProps] = strings.ToLower(name)
		r.nProps++
		return r.nProps - 1
	}
	return 0
}

func (r *reader) startNode(format string) { r.level = append(r.level, format) }

func (r *reader) endNode() {
	if len(r.level) > 0 {
		r.level = r.level[:len(r.level)-1]
	}
}

// toString decodes Latin-1 bytes to a UTF-8 string, applying the two glyph
// substitutions the Cedar sources rely on ('Ó'→'©', '¬'/'_'→'←').
func toString(in []byte) string {
	var sb strings.Builder
	sb.Grow(len(in))
	for _, ch := range in {
		switch ch {
		case 0xd3:
			sb.WriteRune('©')
		case 0xac, '_':
			sb.WriteRune('←')
		default:
			sb.WriteRune(rune(ch)) // Latin-1 code point == Unicode code point
		}
	}
	return sb.String()
}

func (r *reader) curFormat() string {
	if len(r.level) == 0 {
		return ""
	}
	return r.level[len(r.level)-1]
}

func (r *reader) insertText(raw []byte, length int64, comment bool) {
	if length < 0 {
		length = 0
	}
	if int(length) > len(raw) {
		length = int64(len(raw))
	}
	s := toString(raw[:length])

	if r.isCode {
		lines := strings.Split(s, "\n")
		indent := (len(r.level) - 2) * 4
		if indent < 0 {
			indent = 0
		}
		r.code.WriteString(strings.Repeat(" ", indent))
		for _, line := range lines {
			if comment {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" && !strings.HasPrefix(trimmed, "--") {
					r.code.WriteString("-- ")
				}
				if len(r.level) == 1 && trimmed == "" {
					return
				}
			}
			r.code.WriteString(line)
			r.code.WriteByte('\n')
		}
		return
	}

	// Document rendering: map the node format to a block kind.
	f := r.curFormat()
	switch {
	case strings.HasPrefix(f, "code"):
		r.blocks = append(r.blocks, Block{Kind: Code, Text: s})
	case f == "head":
		level := len(r.level) - 1
		if level < 1 {
			level = 1
		}
		if level > 6 {
			level = 6
		}
		r.blocks = append(r.blocks, Block{Kind: Heading, Level: level, Text: s})
	case strings.HasPrefix(f, "head") && len(f) > 4 && f[4] >= '1' && f[4] <= '6':
		r.blocks = append(r.blocks, Block{Kind: Heading, Level: int(f[4] - '0'), Text: s})
	case comment:
		r.blocks = append(r.blocks, Block{Kind: Quote, Text: s})
	default:
		r.blocks = append(r.blocks, Block{Kind: Paragraph, Text: s})
	}
}
