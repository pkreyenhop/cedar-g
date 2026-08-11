package main

import (
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"

	"cedarg/internal/tioga"
)

// The Levels outline numbers headings the way the Tioga docs illustrate it —
// I. / A. / 1. / a. by depth — so a collapsed document reads as a structured
// outline. Markers are shown only while the Levels bar is open.

// outlineMarkers returns a marker per block, empty for non-heading blocks,
// numbering headings hierarchically by their level.
func outlineMarkers(blocks []tioga.Block) []string {
	markers := make([]string, len(blocks))
	var counters [12]int
	for i, b := range blocks {
		if b.Kind != tioga.Heading {
			continue
		}
		lvl := b.Level
		if lvl < 1 {
			lvl = 1
		}
		if lvl >= len(counters) {
			lvl = len(counters) - 1
		}
		counters[lvl]++
		for k := lvl + 1; k < len(counters); k++ {
			counters[k] = 0
		}
		markers[i] = markerFor(lvl, counters[lvl])
	}
	return markers
}

// markerFor formats an outline marker for a level: I. A. 1. a. i. then arabic.
func markerFor(level, n int) string {
	switch level {
	case 1:
		return toRoman(n) + "."
	case 2:
		return toAlpha(n) + "."
	case 3:
		return strconv.Itoa(n) + "."
	case 4:
		return strings.ToLower(toAlpha(n)) + "."
	case 5:
		return strings.ToLower(toRoman(n)) + "."
	default:
		return strconv.Itoa(n) + "."
	}
}

// toAlpha renders 1→A, 2→B, … 26→Z, 27→AA (uppercase).
func toAlpha(n int) string {
	if n <= 0 {
		return ""
	}
	var b []byte
	for n > 0 {
		n--
		b = append([]byte{byte('A' + n%26)}, b...)
		n /= 26
	}
	return string(b)
}

// toRoman renders a small positive integer as an uppercase Roman numeral.
func toRoman(n int) string {
	if n <= 0 {
		return strconv.Itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// outlineRow prefixes a block with its outline marker in a left gutter.
func (s *gioUI) outlineRow(gtx C, marker string, content layout.Widget) D {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(46), gtx.Dp(46)
			return layout.Inset{Top: 10, Right: 4}.Layout(gtx, func(gtx C) D {
				return s.label(gtx, serifFont, font.Bold, font.Regular, docTextSize-3, marker, cedarBlack, 1)
			})
		}),
		layout.Flexed(1, content),
	)
}
