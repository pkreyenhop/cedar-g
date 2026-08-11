package main

import (
	"image"
	"os"
	"strings"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"cedarg/internal/tioga"
)

// enterStruct turns a document viewer into the structured node editor, wrapping
// its parsed tree in a mutable Doc and selecting the first node.
func (s *gioUI) enterStruct(v *viewer) {
	if v.root == nil {
		v.root = &tioga.Node{}
	}
	v.doc = tioga.NewDoc(v.root)
	v.nodeEds = map[*tioga.Node]*widget.Editor{}
	v.undoStack, v.redoStack = nil, nil
	if ns := v.doc.Nodes(); len(ns) > 0 {
		v.sel = ns[0]
	}
	v.structEdit = true
}

// exitStruct leaves structure editing and rebuilds the read-only block view from
// the (possibly edited) tree.
func (s *gioUI) exitStruct(v *viewer) {
	s.syncEditors(v)
	blocks := tioga.FlattenBlocks(v.root)
	for i := range blocks {
		blocks[i].Text = expandTabs(blocks[i].Text)
	}
	v.blocks = blocks
	v.selBlock = -1
	v.blockClicks = nil
	v.structEdit = false
}

// editorFor returns the (lazily created) text editor for a node, seeded with its
// current text.
func (v *viewer) editorFor(n *tioga.Node) *widget.Editor {
	if v.nodeEds == nil {
		v.nodeEds = map[*tioga.Node]*widget.Editor{}
	}
	ed, ok := v.nodeEds[n]
	if !ok {
		ed = &widget.Editor{}
		ed.SetText(n.Text())
		v.nodeEds[n] = ed
	}
	return ed
}

// syncEditors writes each node editor's text back into its node (as plain text)
// when it has changed, so structural operations act on the current text.
func (s *gioUI) syncEditors(v *viewer) {
	if v.doc == nil {
		return
	}
	for _, n := range v.doc.Nodes() {
		if ed, ok := v.nodeEds[n]; ok && ed.Text() != n.Text() {
			v.doc.SetText(n, ed.Text())
		}
	}
}

// structBody renders the structure editor: a command bar over an indented,
// selectable list of per-node text fields.
func (s *gioUI) structBody(gtx C, v *viewer) D {
	if v.doc == nil {
		s.enterStruct(v)
	}
	nodes := v.orderedNodes()
	children := []layout.FlexChild{
		layout.Rigid(func(gtx C) D { return s.structBar(gtx, v) }),
		layout.Rigid(hrule),
	}
	if v.showEditTool {
		children = append(children,
			layout.Rigid(func(gtx C) D { return s.editToolBar(gtx, v) }),
			layout.Rigid(hrule))
	}
	children = append(children, layout.Flexed(1, func(gtx C) D {
		gtx.Constraints.Min = gtx.Constraints.Max
		return layout.Inset{Top: 4, Right: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return s.scrollList(gtx, &v.sc, len(nodes), func(gtx C, i int) D {
				return s.nodeRow(gtx, v, nodes[i].n, nodes[i].depth)
			})
		})
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// editToolBar is the Cedar EditTool: the selected node's format (Get/Set) and a
// palette of looks to apply.
func (s *gioUI) editToolBar(gtx C, v *viewer) D {
	v.formatEd.SingleLine = true
	// Load the selected node's format into the field when the selection changes.
	if v.sel != v.fmtSel {
		v.formatEd.SetText("")
		if v.sel != nil {
			v.formatEd.SetText(v.sel.Format)
		}
		v.fmtSel = v.sel
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	btn := func(b *widget.Clickable, label string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 3}.Layout(gtx, func(gtx C) D { return s.flatButton(gtx, b, label) })
		})
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.UniformInset(3).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D {
							return s.label(gtx, serifFont, font.Bold, font.Regular, 12, "Format:", cedarBlack, 1)
						})
					}),
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(150), gtx.Dp(150)
						return bordered(gtx, func(gtx C) D {
							return layout.UniformInset(2).Layout(gtx, func(gtx C) D {
								fe := material.Editor(s.th, &v.formatEd, "format")
								fe.Color = cedarBlack
								fe.Font = monoFont
								fe.TextSize = s.sp(12)
								return fe.Layout(gtx)
							})
						})
					}),
					btn(&v.bFmtGet, "Get"),
					btn(&v.bFmtSet, "Set"),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(10), 0)} }),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D {
							return s.label(gtx, serifFont, font.Bold, font.Regular, 12, "Looks:", cedarBlack, 1)
						})
					}),
					btn(&v.bLkU, "Under"),
					btn(&v.bLkE, "SmallCap"),
					btn(&v.bLkK, "Code"),
					btn(&v.bLkX, "Strike"),
					btn(&v.bLkH, "Super"),
					btn(&v.bLkL, "Sub"),
				)
			})
		}),
	)
}

type nodeAt struct {
	n     *tioga.Node
	depth int
}

func (v *viewer) orderedNodes() []nodeAt {
	var out []nodeAt
	v.doc.Walk(func(n *tioga.Node, depth int) { out = append(out, nodeAt{n, depth}) })
	return out
}

// structBar is the structure editor's command row: the tree operations and the
// look toggles, acting on the selected node.
func (s *gioUI) structBar(gtx C, v *viewer) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	btn := func(b *widget.Clickable, label string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 3}.Layout(gtx, func(gtx C) D { return s.flatButton(gtx, b, label) })
		})
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.UniformInset(3).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					btn(&v.bNewSib, "+Sibling"),
					btn(&v.bNewChild, "+Child"),
					btn(&v.bNest, "Nest→"),
					btn(&v.bUnnest, "←Unnest"),
					btn(&v.bDel, "Delete"),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(8), 0)} }),
					btn(&v.bBold, "Bold"),
					btn(&v.bItalic, "Italic"),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(8), 0)} }),
					btn(&v.bUndo, "Undo"),
					btn(&v.bRedo, "Redo"),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(8), 0)} }),
					btn(&v.bEditTool, "EditTool"),
					btn(&v.bStructSave, "Save"),
					layout.Rigid(func(gtx C) D {
						if v.saveMsg == "" {
							return D{}
						}
						return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
							return s.label(gtx, serifFont, font.Normal, font.Regular, 12, v.saveMsg, cedarBlack, 1)
						})
					}),
				)
			})
		}),
	)
}

// nodeRow lays out one node: an indented, editable text field, its format label,
// and a selection highlight. Clicking or focusing the field selects the node.
func (s *gioUI) nodeRow(gtx C, v *viewer, n *tioga.Node, depth int) D {
	ed := v.editorFor(n)
	selected := v.sel == n

	if v.focusNode == n {
		gtx.Execute(key.FocusCmd{Tag: ed})
		v.focusNode = nil
	}
	// Sync selection to keyboard focus, but not while a programmatic focus move is
	// pending — the old editor is still focused for one frame and would otherwise
	// revert the selection.
	if v.focusNode == nil && gtx.Focused(ed) {
		v.sel = n
	}

	indent := unit.Dp(float32((depth - 1) * indentStep))
	row := func(gtx C) D {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				// A small format tag in the gutter names the node's format.
				tag := n.Format
				if tag == "" {
					tag = "¶"
				}
				gtx.Constraints.Min.X = gtx.Dp(72)
				gtx.Constraints.Max.X = gtx.Dp(72)
				return s.label(gtx, monoFont, font.Normal, font.Regular, 11, tag, cedarGreyMid, 1)
			}),
			layout.Flexed(1, func(gtx C) D {
				me := material.Editor(s.th, ed, "")
				me.Color = cedarBlack
				me.SelectionColor = cedarGreyMid
				me.Font = serifFont
				me.TextSize = s.sp(docTextSize - 2)
				return me.Layout(gtx)
			}),
		)
	}

	return layout.Inset{Top: 2, Bottom: 2, Left: indent}.Layout(gtx, func(gtx C) D {
		if !selected {
			return row(gtx)
		}
		// Highlight the selected node with a thin left bar and light background.
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx C) D {
				fillAt(gtx, cedarSelBg, image.Rectangle{Max: gtx.Constraints.Min})
				fillAt(gtx, cedarBlack, image.Rect(0, 0, gtx.Dp(2), gtx.Constraints.Min.Y))
				return D{Size: gtx.Constraints.Min}
			}),
			layout.Stacked(func(gtx C) D {
				return layout.Inset{Left: 6}.Layout(gtx, row)
			}),
		)
	})
}

// processStruct handles the structure editor's commands. It syncs editor text
// into the tree first, then applies the op to the selected node.
func (s *gioUI) processStruct(gtx C, v *viewer) {
	if v.bStruct.Clicked(gtx) {
		if v.structEdit {
			s.exitStruct(v)
		} else {
			s.enterStruct(v)
		}
		return
	}
	if !v.structEdit || v.doc == nil {
		return
	}
	s.processStructKeys(gtx, v)
	if v.bUndo.Clicked(gtx) {
		s.undo(v)
		return
	}
	if v.bRedo.Clicked(gtx) {
		s.redo(v)
		return
	}
	// beginEdit syncs pending editor text into the tree and snapshots it for undo,
	// just before a structural or look change.
	act := func() { s.beginEdit(v) }

	switch {
	case v.bNewSib.Clicked(gtx):
		act()
		format := "body"
		if v.sel != nil {
			format = v.sel.Format
		}
		n := v.doc.InsertSibling(v.sel, tioga.NewNode(format, ""))
		v.sel, v.focusNode = n, n
	case v.bNewChild.Clicked(gtx):
		act()
		n := v.doc.InsertChild(v.sel, tioga.NewNode("body", ""))
		v.sel, v.focusNode = n, n
	case v.bNest.Clicked(gtx):
		act()
		if v.sel != nil {
			v.doc.Nest(v.sel)
			v.focusNode = v.sel
		}
	case v.bUnnest.Clicked(gtx):
		act()
		if v.sel != nil {
			v.doc.Unnest(v.sel)
			v.focusNode = v.sel
		}
	case v.bDel.Clicked(gtx):
		act()
		if v.sel != nil {
			next := v.doc.Delete(v.sel)
			delete(v.nodeEds, v.sel)
			v.sel, v.focusNode = next, next
		}
	case v.bBold.Clicked(gtx):
		s.applyLook(v, 'b')
	case v.bItalic.Clicked(gtx):
		s.applyLook(v, 'i')
	case v.bStructSave.Clicked(gtx):
		act()
		s.saveStruct(v)
	case v.bEditTool.Clicked(gtx):
		v.showEditTool = !v.showEditTool
	case v.bFmtGet.Clicked(gtx):
		if v.sel != nil {
			v.formatEd.SetText(v.sel.Format)
		}
	case v.bFmtSet.Clicked(gtx):
		if v.sel != nil {
			act()
			v.doc.SetFormat(v.sel, strings.TrimSpace(v.formatEd.Text()))
		}
	case v.bLkU.Clicked(gtx):
		s.applyLook(v, 'u')
	case v.bLkE.Clicked(gtx):
		s.applyLook(v, 'e')
	case v.bLkK.Clicked(gtx):
		s.applyLook(v, 'k')
	case v.bLkX.Clicked(gtx):
		s.applyLook(v, 'x')
	case v.bLkH.Clicked(gtx):
		s.applyLook(v, 'h')
	case v.bLkL.Clicked(gtx):
		s.applyLook(v, 'l')
	}
}

// applyLook toggles a look on the current text selection in the selected node's
// editor, or on the whole node when nothing is selected (both undoable).
func (s *gioUI) applyLook(v *viewer, letter byte) {
	if v.sel == nil {
		return
	}
	start, end := v.editorFor(v.sel).Selection()
	if start > end {
		start, end = end, start
	}
	s.beginEdit(v)
	if end > start {
		v.doc.ApplyLookRange(v.sel, start, end, letter)
	} else {
		v.doc.ToggleLook(v.sel, letter)
	}
}

// beginEdit records the tree for undo just before a mutation, after folding in
// any pending editor text.
func (s *gioUI) beginEdit(v *viewer) {
	s.syncEditors(v)
	const maxUndo = 100
	v.undoStack = append(v.undoStack, tioga.CloneNode(v.root))
	if len(v.undoStack) > maxUndo {
		v.undoStack = v.undoStack[len(v.undoStack)-maxUndo:]
	}
	v.redoStack = nil
}

// restore swaps the current tree for snap and rebuilds the editing state.
func (s *gioUI) restore(v *viewer, snap *tioga.Node) {
	v.root = snap
	v.doc = tioga.NewDoc(v.root)
	v.nodeEds = map[*tioga.Node]*widget.Editor{}
	if ns := v.doc.Nodes(); len(ns) > 0 {
		v.sel, v.focusNode = ns[0], ns[0]
	} else {
		v.sel, v.focusNode = nil, nil
	}
}

func (s *gioUI) undo(v *viewer) {
	if len(v.undoStack) == 0 {
		return
	}
	s.syncEditors(v)
	v.redoStack = append(v.redoStack, tioga.CloneNode(v.root))
	snap := v.undoStack[len(v.undoStack)-1]
	v.undoStack = v.undoStack[:len(v.undoStack)-1]
	s.restore(v, snap)
}

func (s *gioUI) redo(v *viewer) {
	if len(v.redoStack) == 0 {
		return
	}
	v.undoStack = append(v.undoStack, tioga.CloneNode(v.root))
	snap := v.redoStack[len(v.redoStack)-1]
	v.redoStack = v.redoStack[:len(v.redoStack)-1]
	s.restore(v, snap)
}

// processStructKeys handles Tioga's structural keyboard gestures on the selected
// node's editor: CTRL-RETURN new sibling, CTRL-I new child (insert+nest),
// CTRL-SHIFT-I new unnested node, CTRL-N nest, CTRL-SHIFT-N unnest.
func (s *gioUI) processStructKeys(gtx C, v *viewer) {
	if v.sel == nil {
		return
	}
	ed := v.editorFor(v.sel)
	filters := []event.Filter{
		key.Filter{Focus: ed, Name: key.NameReturn, Required: key.ModCtrl, Optional: key.ModShift},
		key.Filter{Focus: ed, Name: "I", Required: key.ModCtrl, Optional: key.ModShift},
		key.Filter{Focus: ed, Name: "N", Required: key.ModCtrl, Optional: key.ModShift},
		key.Filter{Focus: ed, Name: "Z", Required: key.ModCtrl, Optional: key.ModShift},
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		ke, ok := ev.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		shift := ke.Modifiers.Contain(key.ModShift)
		if ke.Name == "Z" { // undo / redo
			if shift {
				s.redo(v)
			} else {
				s.undo(v)
			}
			continue
		}
		s.beginEdit(v)
		switch ke.Name {
		case key.NameReturn:
			n := v.doc.InsertSibling(v.sel, tioga.NewNode(v.sel.Format, ""))
			v.sel, v.focusNode = n, n
		case "I":
			n := v.doc.InsertChild(v.sel, tioga.NewNode("body", ""))
			if shift { // insert unnested: move it out to the parent's level
				v.doc.Unnest(n)
			}
			v.sel, v.focusNode = n, n
		case "N":
			if shift {
				v.doc.Unnest(v.sel)
			} else {
				v.doc.Nest(v.sel)
			}
			v.focusNode = v.sel
		}
	}
}

// saveStruct writes the edited node tree back to disk as a real Tioga file
// (nesting, formats and looks preserved).
func (s *gioUI) saveStruct(v *viewer) {
	if v.root == nil || v.path == "" {
		v.saveMsg = "nothing to save"
		return
	}
	if err := os.WriteFile(v.path, tioga.EncodeDoc(v.root), 0o644); err != nil {
		v.saveMsg = "save failed: " + err.Error()
		return
	}
	v.saveMsg = "saved " + v.rel
	s.setMessage(v.saveMsg)
}
