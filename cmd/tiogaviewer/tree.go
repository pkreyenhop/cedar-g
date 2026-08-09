package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
)

// fileSuffixes are the file kinds shown in the tree.
var fileSuffixes = []string{"tioga", "mesa", "df", "require", "profile", "depends"}

func matchFile(name string) bool {
	for _, sfx := range fileSuffixes {
		if strings.HasSuffix(name, "."+sfx) || strings.Contains(name, "."+sfx+"!") {
			return true
		}
	}
	return false
}

// tree is a lazy file browser: a flattened list of visible rows driven by an
// expanded-set and a directory-listing cache.
type tree struct {
	root       string
	expanded   map[string]bool
	childCache map[string][]string
	clicks     map[string]*widget.Clickable
	list       widget.List
	onOpen     func(path string)
}

type treeRow struct {
	path  string
	depth int
	isDir bool
}

func newTree(onOpen func(string)) *tree {
	t := &tree{
		expanded:   map[string]bool{},
		childCache: map[string][]string{},
		clicks:     map[string]*widget.Clickable{},
		onOpen:     onOpen,
	}
	t.list.Axis = layout.Vertical
	return t
}

func (t *tree) setRoot(path string) {
	t.root = path
	t.expanded = map[string]bool{path: true}
	t.childCache = map[string][]string{}
}

func (t *tree) children(dir string) []string {
	if c, ok := t.childCache[dir]; ok {
		return c
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.childCache[dir] = nil
		return nil
	}
	var dirs, files []string
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			dirs = append(dirs, full)
		} else if matchFile(e.Name()) {
			files = append(files, full)
		}
	}
	sort.Strings(dirs)
	sort.Strings(files)
	c := append(dirs, files...)
	t.childCache[dir] = c
	return c
}

// rows flattens the currently-visible tree.
func (t *tree) rows() []treeRow {
	if t.root == "" {
		return nil
	}
	var out []treeRow
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		for _, p := range t.children(dir) {
			info, err := os.Stat(p)
			isDir := err == nil && info.IsDir()
			out = append(out, treeRow{path: p, depth: depth, isDir: isDir})
			if isDir && t.expanded[p] {
				walk(p, depth+1)
			}
		}
	}
	walk(t.root, 0)
	return out
}

func (t *tree) click(path string) *widget.Clickable {
	if c, ok := t.clicks[path]; ok {
		return c
	}
	c := &widget.Clickable{}
	t.clicks[path] = c
	return c
}

// update processes row clicks: toggling directories and opening files.
func (t *tree) update(gtx C, rows []treeRow) {
	for _, r := range rows {
		c := t.click(r.path)
		if c.Clicked(gtx) {
			if r.isDir {
				t.expanded[r.path] = !t.expanded[r.path]
			} else if t.onOpen != nil {
				t.onOpen(r.path)
			}
		}
	}
}

func (s *gioUI) layoutTree(gtx C, t *tree) D {
	rows := t.rows()
	t.update(gtx, rows)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return s.captionStrip(gtx, "Files") }),
		layout.Rigid(hrule),
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx C) D { return fill(gtx, cedarWhite, gtx.Constraints.Min) }),
				layout.Stacked(func(gtx C) D {
					return s.scrollList(gtx, &t.list, len(rows), func(gtx C, i int) D {
						return s.treeRow(gtx, t, rows[i])
					})
				}),
			)
		}),
	)
}

func (s *gioUI) treeRow(gtx C, t *tree, r treeRow) D {
	c := t.click(r.path)
	return c.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		bg := cedarWhite
		if c.Hovered() {
			bg = cedarGrey
		}
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx C) D { return fill(gtx, bg, gtx.Constraints.Min) }),
			layout.Stacked(func(gtx C) D {
				indent := unit.Dp(4 + r.depth*14)
				marker := "   "
				if r.isDir {
					if t.expanded[r.path] {
						marker = "-  "
					} else {
						marker = "+  "
					}
				}
				weight := font.Normal
				if r.isDir {
					weight = font.Bold
				}
				return layout.Inset{Left: indent, Top: 1, Bottom: 1}.Layout(gtx, func(gtx C) D {
					return s.label(gtx, serifFont, weight, font.Regular, 13, marker+filepath.Base(r.path), cedarBlack, 1)
				})
			}),
		)
	})
}
