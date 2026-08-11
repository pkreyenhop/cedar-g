package main

import (
	"strings"

	"gioui.org/font"

	"cedarg/internal/tioga"
)

// lookVis is the resolved visual effect of a run's Tioga looks in the default
// Cedar style: the face (weight/slant/pitch), a size scale, a baseline shift
// (for super/subscript), and decorations.
type lookVis struct {
	fnt       font.Font
	sizeScale float32 // multiplies the base size
	dy        float32 // baseline shift as a fraction of size: <0 up (super), >0 down (sub)
	underline bool
	strike    bool
	smallCaps bool
}

// lookVisual maps a run's looks onto visual attributes. The universal looks
// b/i/u render the same in every style; the rest are the default Cedar style's
// conventions inferred from how they are used across the sources:
//
//	b  bold          i  italic        u  underline
//	k  code (mono)   p  fixed pitch   e  emphasis → small caps (product names)
//	x  strikeout     h  superscript   l  subscript   (h/l: half-line up/down)
//
// Letters without a confident meaning are left unstyled (rendered plainly), so
// they never make text look wrong; the table is easy to extend as more are
// pinned down.
func lookVisual(base font.Font, lk tioga.Look) lookVis {
	v := lookVis{fnt: base, sizeScale: 1}
	if lk.Bold() {
		v.fnt.Weight = font.Bold
	}
	if lk.Italic() {
		v.fnt.Style = font.Italic
	}
	if lk.Has('k') || lk.Has('p') {
		v.fnt.Typeface = "Mono"
	}
	if lk.Underline() {
		v.underline = true
	}
	if lk.Has('x') {
		v.strike = true
	}
	if lk.Has('e') {
		v.smallCaps = true
		v.sizeScale = 0.82
	}
	switch {
	case lk.Has('h'): // superscript
		v.sizeScale, v.dy = 0.72, -0.32
	case lk.Has('l'): // subscript
		v.sizeScale, v.dy = 0.72, 0.18
	}
	return v
}

// applyCaps upper-cases text for the small-caps approximation (uppercase at a
// reduced size).
func applyCaps(text string, v lookVis) string {
	if v.smallCaps {
		return strings.ToUpper(text)
	}
	return text
}
