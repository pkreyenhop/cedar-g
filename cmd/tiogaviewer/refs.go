package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/widget"
)

// Tioga documents cross-reference other documents and sources by file name
// (e.g. "TiogaDoc.tioga", "AISIO.mesa", "Graphics3d-Suite.df"). We surface those
// as clickable references that open the target, resolving Cedar "!N" versions.

// refRe matches a Cedar file reference token.
var refRe = regexp.MustCompile(`[\w][\w.-]*\.(tioga|mesa|df|style|config|cm|bcd|gargoyle)\b`)

// docRef is one discovered file reference.
type docRef struct {
	name  string
	click widget.Clickable
}

// computeRefs scans the document's blocks for file references (unique, sorted).
func (v *viewer) computeRefs() {
	seen := map[string]bool{}
	for _, b := range v.blocks {
		for _, m := range refRe.FindAllString(b.Text, -1) {
			seen[m] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	v.refs = v.refs[:0]
	for _, n := range names {
		v.refs = append(v.refs, &docRef{name: n})
	}
}

// resolveRef finds the file a reference names, near the document then under the
// root, tolerating a Cedar "!N" version suffix on candidates.
func (s *gioUI) resolveRef(v *viewer, name string) (string, bool) {
	dirs := []string{}
	if v.path != "" {
		dirs = append(dirs, filepath.Dir(v.path))
	}
	if s.root != "" {
		dirs = append(dirs, s.root)
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
		// Match a versioned file "name!N" in the directory.
		if entries, err := os.ReadDir(d); err == nil {
			for _, e := range entries {
				if b := e.Name(); b == name || strings.HasPrefix(b, name+"!") {
					return filepath.Join(d, b), true
				}
			}
		}
	}
	return "", false
}

// processRefs handles the Refs/Print buttons and reference clicks.
func (s *gioUI) processRefs(gtx C, v *viewer) {
	if v.pendingRef != "" {
		name := v.pendingRef
		v.pendingRef = ""
		if p, ok := s.resolveRef(v, name); ok {
			s.openFile(p)
			s.setMessage("opened " + name)
		} else {
			s.setMessage("not found: " + name)
		}
	}
	if v.bPrint.Clicked(gtx) {
		s.exportDoc(v)
	}
	if v.bRefs.Clicked(gtx) {
		v.showRefs = !v.showRefs
		if v.showRefs {
			v.computeRefs()
		}
	}
	for _, r := range v.refs {
		if r.click.Clicked(gtx) {
			if p, ok := s.resolveRef(v, r.name); ok {
				s.openFile(p)
				v.saveMsg = "opened " + r.name
				s.setMessage(v.saveMsg)
			} else {
				v.saveMsg = "not found: " + r.name
				s.setMessage(v.saveMsg)
			}
		}
	}
}

// refsBar shows the document's file references as clickable buttons.
func (s *gioUI) refsBar(gtx C, v *viewer) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.UniformInset(3).Layout(gtx, func(gtx C) D {
				children := []layout.FlexChild{
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D {
							return s.label(gtx, serifFont, font.Bold, font.Regular, 12, "References:", cedarBlack, 1)
						})
					}),
				}
				if len(v.refs) == 0 {
					children = append(children, layout.Rigid(func(gtx C) D {
						return s.label(gtx, serifFont, font.Normal, font.Regular, 12, "(none)", cedarBlack, 1)
					}))
				}
				for _, r := range v.refs {
					r := r
					children = append(children, layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: 3}.Layout(gtx, func(gtx C) D {
							return s.flatButton(gtx, &r.click, r.name)
						})
					}))
				}
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
			})
		}),
	)
}
