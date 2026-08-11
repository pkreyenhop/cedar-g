package main

import (
	"testing"

	"gioui.org/font"

	"cedarg/internal/tioga"
)

func lk(letters ...byte) tioga.Look {
	var l tioga.Look
	for _, c := range letters {
		l |= 1 << (31 - (c - 'a'))
	}
	return l
}

func TestLookVisual(t *testing.T) {
	base := font.Font{Typeface: "Serif"}

	if v := lookVisual(base, lk('b', 'i')); v.fnt.Weight != font.Bold || v.fnt.Style != font.Italic {
		t.Fatalf("bold-italic: %+v", v.fnt)
	}
	if v := lookVisual(base, lk('u')); !v.underline {
		t.Fatalf("underline not set")
	}
	if v := lookVisual(base, lk('x')); !v.strike {
		t.Fatalf("strikeout not set")
	}
	if v := lookVisual(base, lk('k')); v.fnt.Typeface != "Mono" {
		t.Fatalf("k should be mono")
	}
	if v := lookVisual(base, lk('p')); v.fnt.Typeface != "Mono" {
		t.Fatalf("p should be mono")
	}
	if v := lookVisual(base, lk('e')); !v.smallCaps || v.sizeScale >= 1 {
		t.Fatalf("e should be small-caps, reduced size: %+v", v)
	}
	if got := applyCaps("Cedar", lookVisual(base, lk('e'))); got != "CEDAR" {
		t.Fatalf("small-caps text = %q", got)
	}
	if v := lookVisual(base, lk('h')); v.dy >= 0 || v.sizeScale >= 1 {
		t.Fatalf("h should be superscript (up, smaller): %+v", v)
	}
	if v := lookVisual(base, lk('l')); v.dy <= 0 {
		t.Fatalf("l should be subscript (down): %+v", v)
	}
	// An unassigned look leaves the base untouched.
	if v := lookVisual(base, lk('z')); v.fnt != base || v.underline || v.smallCaps || v.dy != 0 {
		t.Fatalf("unassigned look should be plain: %+v", v)
	}
}
