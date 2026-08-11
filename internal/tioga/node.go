package tioga

import "strings"

// Look is the raw Tioga "looks" bitset carried by a run of characters. Looks are
// character-level appearance attributes (bold, italic, underline, …) that are
// independent of a node's structural format. Each look is named by a lower-case
// code letter; the bit for letter x is 1<<(31-(x-'a')), matching the on-disk
// encoding (see getLookChars). The interpretation of most letters is defined by
// the document's *style* (a later phase); the three universal, style-independent
// looks — b(old), i(talic), u(nderline) — are exposed directly here.
type Look uint32

// Has reports whether the given look code letter is set.
func (l Look) Has(letter byte) bool {
	if letter < 'a' || letter > 'a'+31 {
		return false
	}
	return l&(1<<(31-(letter-'a'))) != 0
}

// Bold, Italic and Underline are the universal looks, rendered the same way in
// every Cedar style.
func (l Look) Bold() bool      { return l.Has('b') }
func (l Look) Italic() bool    { return l.Has('i') }
func (l Look) Underline() bool { return l.Has('u') }

// Letters returns the set look code letters in ascending order (e.g. "bi"),
// preserving the full looks vocabulary for later style interpretation and for
// round-tripping. It returns "" when no looks are set.
func (l Look) Letters() string {
	var sb strings.Builder
	for c := byte('a'); c <= 'a'+31; c++ {
		if l.Has(c) {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// Run is a maximal slice of a node's text sharing one set of looks.
type Run struct {
	Text string
	Look Look
}

// Prop is a Tioga node property: a name (e.g. "postfix", "NewlineDelimiter",
// "Mark", "StyleDef") and its opaque value bytes. Properties carry style-beyond-
// style effects (colours, boxes, artwork references, …); the viewer does not
// interpret them, but preserves them so documents round-trip losslessly. Value
// holds the raw property bytes verbatim.
type Prop struct {
	Name  string
	Value string
}

// Node is one node of a Tioga document tree. A document is a tree of nodes;
// nesting (a node's Children) is what the editor shows as indentation. Format is
// the node's named structural format ("" for the default/root, else "title",
// "head", "body", "block", "item", …). Runs is the node's text split into
// look-runs; Comment is an optional attached comment, also split into runs.
type Node struct {
	Format   string
	Runs     []Run
	Comment  []Run
	Children []*Node
	Props    []Prop
}

// Text returns the node's text with looks flattened away.
func (n *Node) Text() string {
	var sb strings.Builder
	for _, r := range n.Runs {
		sb.WriteString(r.Text)
	}
	return sb.String()
}

// splitRuns divides text (as runes) into runs according to a sequence of
// (look, length) spans. Any characters beyond the spans' total length become a
// final plain run; spans past the end of the text are dropped. A nil/empty spec
// yields a single plain run (or no run for empty text).
func splitRuns(text string, spec []runSpan) []Run {
	if text == "" {
		return nil
	}
	rs := []rune(text)
	if len(spec) == 0 {
		return []Run{{Text: text}}
	}
	var out []Run
	pos := 0
	for _, sp := range spec {
		if pos >= len(rs) {
			break
		}
		end := pos + int(sp.n)
		if end > len(rs) {
			end = len(rs)
		}
		if end > pos {
			out = append(out, Run{Text: string(rs[pos:end]), Look: sp.look})
			pos = end
		}
	}
	if pos < len(rs) {
		out = append(out, Run{Text: string(rs[pos:])})
	}
	return out
}

// runSpan is one (look, run-length) pair from an opRuns control record.
type runSpan struct {
	look Look
	n    int64
}
