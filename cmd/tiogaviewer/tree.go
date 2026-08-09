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

// treeEntry is a cached directory child (its kind known from ReadDir, so we
// never re-stat).
type treeEntry struct {
	path  string
	isDir bool
}

// tree is a lazy file browser: a flattened list of visible rows driven by an
// expanded-set and a directory-listing cache. The flattened rows are cached and
// only rebuilt when the expanded-set or root changes (not every frame).
type tree struct {
	root       string
	expanded   map[string]bool
	childCache map[string][]treeEntry
	clicks     map[string]*widget.Clickable
	list       widget.List
	onOpen     func(path string)

	rowCache  []treeRow
	rowsValid bool
}

type treeRow struct {
	path  string
	depth int
	isDir bool
}

func newTree(onOpen func(string)) *tree {
	t := &tree{
		expanded:   map[string]bool{},
		childCache: map[string][]treeEntry{},
		clicks:     map[string]*widget.Clickable{},
		onOpen:     onOpen,
	}
	t.list.Axis = layout.Vertical
	return t
}

func (t *tree) setRoot(path string) {
	t.root = path
	t.expanded = map[string]bool{path: true}
	t.childCache = map[string][]treeEntry{}
	t.rowsValid = false
}

// toggle expands/collapses a directory and invalidates the flattened rows.
func (t *tree) toggle(path string) {
	t.expanded[path] = !t.expanded[path]
	t.rowsValid = false
}

// children lists a directory once and caches it, taking the file/dir kind from
// the directory entry (no per-child stat).
func (t *tree) children(dir string) []treeEntry {
	if c, ok := t.childCache[dir]; ok {
		return c
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.childCache[dir] = nil
		return nil
	}
	var dirs, files []treeEntry
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			dirs = append(dirs, treeEntry{full, true})
		} else if matchFile(e.Name()) {
			files = append(files, treeEntry{full, false})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].path < dirs[j].path })
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	c := append(dirs, files...)
	t.childCache[dir] = c
	return c
}

// rows returns the flattened visible tree, rebuilding only when invalidated.
func (t *tree) rows() []treeRow {
	if t.rowsValid {
		return t.rowCache
	}
	var out []treeRow
	if t.root != "" {
		var walk func(dir string, depth int)
		walk = func(dir string, depth int) {
			for _, e := range t.children(dir) {
				out = append(out, treeRow{path: e.path, depth: depth, isDir: e.isDir})
				if e.isDir && t.expanded[e.path] {
					walk(e.path, depth+1)
				}
			}
		}
		walk(t.root, 0)
	}
	t.rowCache = out
	t.rowsValid = true
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
				t.toggle(r.path)
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
