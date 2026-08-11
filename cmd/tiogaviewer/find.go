package main

import (
	"image/color"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget/material"

	"cedarg/internal/tioga"
)

// findBg tints the block that holds the current search match.
var findBg = color.NRGBA{R: 0xff, G: 0xf3, B: 0x9e, A: 0xff}

// updateFind recomputes matches when the query changes and handles Find / Next /
// Prev, scrolling the current match into view. Search runs over the visible
// (level-filtered) blocks, so it naturally respects the Levels outline.
func (s *gioUI) updateFind(gtx C, v *viewer) {
	if v.bFind.Clicked(gtx) {
		v.findOpen = !v.findOpen
		if v.findOpen {
			gtx.Execute(key.FocusCmd{Tag: &v.findEd})
		}
	}
	if !v.findOpen {
		return
	}
	if q := strings.TrimSpace(v.findEd.Text()); q != v.findQuery {
		v.findQuery = q
		v.recomputeMatches(v.visibleBlocks())
		v.findIdx = 0
		s.scrollToMatch(v)
	}
	if v.bFindNext.Clicked(gtx) && len(v.findMatches) > 0 {
		v.findIdx = (v.findIdx + 1) % len(v.findMatches)
		s.scrollToMatch(v)
	}
	if v.bFindPrev.Clicked(gtx) && len(v.findMatches) > 0 {
		v.findIdx = (v.findIdx - 1 + len(v.findMatches)) % len(v.findMatches)
		s.scrollToMatch(v)
	}
}

// recomputeMatches finds the visible blocks whose text contains the query
// (case insensitive).
func (v *viewer) recomputeMatches(blocks []tioga.Block) {
	v.findMatches = v.findMatches[:0]
	if v.findQuery == "" {
		return
	}
	q := strings.ToLower(v.findQuery)
	for i, b := range blocks {
		if strings.Contains(strings.ToLower(b.Text), q) {
			v.findMatches = append(v.findMatches, i)
		}
	}
}

// currentMatchBlock is the visible-block index of the current match, or -1.
func (v *viewer) currentMatchBlock() int {
	if !v.findOpen || len(v.findMatches) == 0 || v.findIdx >= len(v.findMatches) {
		return -1
	}
	return v.findMatches[v.findIdx]
}

func (s *gioUI) scrollToMatch(v *viewer) {
	if b := v.currentMatchBlock(); b >= 0 {
		v.sc.list.List.Position.First = b
		v.sc.list.List.Position.Offset = 0
	}
}

// findBar is the search UI revealed by the Find button: a query field, match
// counter and next/previous controls.
func (s *gioUI) findBar(gtx C, v *viewer) D {
	v.findEd.SingleLine = true
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	count := ""
	if v.findQuery != "" {
		if len(v.findMatches) == 0 {
			count = "no matches"
		} else {
			count = strconv.Itoa(v.findIdx+1) + " of " + strconv.Itoa(len(v.findMatches))
		}
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.UniformInset(4).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D {
							return s.label(gtx, serifFont, font.Bold, font.Regular, 13, "Find:", cedarBlack, 1)
						})
					}),
					layout.Flexed(1, func(gtx C) D {
						ed := material.Editor(s.th, &v.findEd, "search…")
						ed.Color = cedarBlack
						ed.HintColor = cedarGreyMid
						ed.SelectionColor = cedarGreyMid
						ed.Font = serifFont
						ed.TextSize = s.sp(13)
						return ed.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D { return s.flatButton(gtx, &v.bFindPrev, "Prev") })
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 3}.Layout(gtx, func(gtx C) D { return s.flatButton(gtx, &v.bFindNext, "Next") })
					}),
					layout.Rigid(func(gtx C) D {
						if count == "" {
							return D{}
						}
						return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
							return s.label(gtx, serifFont, font.Normal, font.Regular, 12, count, cedarBlack, 1)
						})
					}),
				)
			})
		}),
	)
}
