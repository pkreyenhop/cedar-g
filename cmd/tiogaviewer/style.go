package main

import (
	"gioui.org/font"
	"gioui.org/text"
	"gioui.org/unit"
)

// blockStyle is the resolved visual format for one document node: the base face
// (weight/slant baked in), size, alignment, the space around it, and its left
// indent. It is Tioga's "format" — the node's structural recipe — as interpreted
// by the default Cedar style.
type blockStyle struct {
	fnt    font.Font
	size   float32
	align  text.Alignment
	above  unit.Dp
	below  unit.Dp
	indent unit.Dp // the format's own left indent (nesting indent is added on top)
}

// indentStep is the left offset added per level of node nesting, so the tree
// structure shows up as indentation the way Tioga renders it.
const indentStep = 12

// maxIndentDepth caps the nesting indent so deeply-buried paragraphs stay
// readable rather than marching off the right edge.
const maxIndentDepth = 6

// docStyle resolves a block's named format (and nesting depth) to a blockStyle,
// implementing the default Cedar style over the formats that actually occur in
// the Cedar sources (body/block/head/item/indent/abstract/title/…). Unknown
// formats fall back to the body paragraph style.
func docStyle(format string, depth int) blockStyle {
	serif := func(w font.Weight, s font.Style) font.Font {
		return font.Font{Typeface: "Serif", Weight: w, Style: s}
	}
	mono := font.Font{Typeface: "Mono"}

	// Body paragraph: the default everything falls back to.
	st := blockStyle{fnt: serif(font.Normal, font.Regular), size: docTextSize, above: 3, below: 3}

	switch {
	case format == "title":
		st.fnt = serif(font.Bold, font.Regular)
		st.size = 26
		st.align = text.Middle
		st.above, st.below = 14, 8
	case format == "subtitle":
		st.fnt = serif(font.Bold, font.Italic)
		st.size = 19
		st.align = text.Middle
		st.above, st.below = 2, 8
	case format == "authors", format == "memohead":
		st.size = 15
		st.align = text.Middle
		st.below = 6
	case format == "boilerplate", format == "copyrightNotice":
		st.size = 13
		st.align = text.Middle
	case format == "abstract":
		st.size = 16
		st.indent = 24
		st.above, st.below = 6, 6
	case isHeadFormat(format):
		st.fnt = serif(font.Bold, font.Regular)
		st.size = headSize(headLevel(format, depth))
		st.above, st.below = 12, 4
	case format == "note", format == "caption", format == "reference", format == "footnote":
		st.fnt = serif(font.Normal, font.Italic)
		st.size = 15
		st.indent = 18
	case format == "center":
		st.align = text.Middle
	case isCodeFormat(format):
		st.fnt = mono
		st.size = 14
		st.above, st.below = 1, 1
	case format == "block", format == "indent", format == "quotation", format == "display":
		st.indent = 24
	case format == "item", format == "lead1":
		st.indent = 18
	case format == "item1":
		st.indent = 36
	}

	// Nesting indent: the tree's depth shown as indentation, on top of the
	// format's own indent. Depth 1 is top level and adds nothing.
	d := depth - 1
	if d > maxIndentDepth {
		d = maxIndentDepth
	}
	if d > 0 {
		st.indent += unit.Dp(d * indentStep)
	}
	return st
}

// isHeadFormat reports whether format is a section-heading format ("head",
// "head2", "memohead"…). "head" with no suffix takes its level from nesting.
func isHeadFormat(format string) bool {
	return format == "head" || (len(format) == 5 && format[:4] == "head" && format[4] >= '1' && format[4] <= '9')
}

// headLevel returns the 1..5 heading level for a head format: from its digit
// suffix, or from nesting depth for a bare "head".
func headLevel(format string, depth int) int {
	if len(format) == 5 && format[4] >= '1' && format[4] <= '9' {
		return int(format[4] - '0')
	}
	lvl := depth - 1
	if lvl < 1 {
		lvl = 1
	}
	return lvl
}

// headSize maps a heading level to a point size (larger for higher headings).
func headSize(level int) float32 {
	switch {
	case level <= 1:
		return 21
	case level == 2:
		return 19
	case level == 3:
		return 17
	case level == 4:
		return 16
	default:
		return 15
	}
}

// isCodeFormat reports whether a format renders as fixed-pitch source/command
// text (the "code", "command", "example", "ascii", "unit" family).
func isCodeFormat(format string) bool {
	switch format {
	case "code", "command", "example", "ascii", "unit", "imp", "proc", "functiondoc":
		return true
	}
	return false
}
