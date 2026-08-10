package main

import (
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
)

// fileSuffixes are the file kinds shown in the tree.
var fileSuffixes = []string{"tioga", "mesa", "df", "require", "profile", "depends", "txt", "md", "go", "c"}

// isTiogaName reports whether a path is a .tioga document (incl. versioned !N).
func isTiogaName(path string) bool {
	b := filepath.Base(path)
	return strings.HasSuffix(b, ".tioga") || strings.Contains(b, ".tioga!")
}

func matchFile(name string) bool {
	for _, sfx := range fileSuffixes {
		if strings.HasSuffix(name, "."+sfx) || strings.Contains(name, "."+sfx+"!") {
			return true
		}
	}
	return false
}

// treeEntry is a cached directory child (kind known from ReadDir, never re-stat).
type treeEntry struct {
	path  string
	isDir bool
}

// tree is a lazy file browser. Directory listings are read on background
// goroutines so a slow filesystem never blocks the UI; the flattened visible
// rows are cached and rebuilt only when the tree changes.
type tree struct {
	root     string
	expanded map[string]bool // UI-thread only
	clicks   map[string]*widget.Clickable
	sc       scroller
	onOpen   func(path string)

	headerHovered bool // reveals the header menu (UI-thread only)

	invalidate func() // wakes the render loop when a background read completes

	mu         sync.Mutex // guards the fields below (touched by goroutines)
	childCache map[string][]treeEntry
	loading    map[string]bool
	rowCache   []treeRow
	rowsValid  bool
	gen        int // bumped on any change; guards stale row caching
}

type treeRow struct {
	path  string
	depth int
	isDir bool
}

func newTree(onOpen func(string)) *tree {
	t := &tree{
		expanded:   map[string]bool{},
		clicks:     map[string]*widget.Clickable{},
		childCache: map[string][]treeEntry{},
		loading:    map[string]bool{},
		onOpen:     onOpen,
	}
	return t
}

func (t *tree) setRoot(path string) {
	t.expanded = map[string]bool{path: true}
	t.mu.Lock()
	t.root = path
	t.childCache = map[string][]treeEntry{}
	t.loading = map[string]bool{}
	t.rowsValid = false
	t.gen++
	t.mu.Unlock()
}

// refresh drops cached directory listings so newly written files appear.
func (t *tree) refresh() {
	t.mu.Lock()
	t.childCache = map[string][]treeEntry{}
	t.loading = map[string]bool{}
	t.rowsValid = false
	t.gen++
	t.mu.Unlock()
}

func (t *tree) rootPath() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.root
}

// toggle expands/collapses a directory and invalidates the flattened rows.
func (t *tree) toggle(path string) {
	t.expanded[path] = !t.expanded[path]
	t.mu.Lock()
	t.rowsValid = false
	t.gen++
	t.mu.Unlock()
}

// readDirEntries lists a directory (off the UI thread), filtered and sorted.
func readDirEntries(dir string) []treeEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
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
	return append(dirs, files...)
}

// children returns a directory's cached listing, or nil while it loads in the
// background (the read never blocks the caller).
func (t *tree) children(dir string) []treeEntry {
	t.mu.Lock()
	if c, ok := t.childCache[dir]; ok {
		t.mu.Unlock()
		return c
	}
	if t.loading[dir] {
		t.mu.Unlock()
		return nil
	}
	t.loading[dir] = true
	t.mu.Unlock()

	store := func(c []treeEntry) {
		t.mu.Lock()
		t.childCache[dir] = c
		delete(t.loading, dir)
		t.rowsValid = false
		t.gen++
		t.mu.Unlock()
	}

	// Without a render loop to wake (tests/headless), read synchronously so the
	// result is available immediately.
	if t.invalidate == nil {
		c := readDirEntries(dir)
		store(c)
		return c
	}

	go func() {
		store(readDirEntries(dir))
		t.invalidate()
	}()
	return nil
}

// rows returns the flattened visible tree, rebuilding only when invalidated.
func (t *tree) rows() []treeRow {
	t.mu.Lock()
	if t.rowsValid {
		c := t.rowCache
		t.mu.Unlock()
		return c
	}
	gen := t.gen
	t.mu.Unlock()

	out := t.buildRows()

	t.mu.Lock()
	if t.gen == gen { // nothing changed while we built: safe to cache
		t.rowCache = out
		t.rowsValid = true
	}
	t.mu.Unlock()
	return out
}

func (t *tree) buildRows() []treeRow {
	root := t.rootPath()
	if root == "" {
		return nil
	}
	var out []treeRow
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		for _, e := range t.children(dir) {
			out = append(out, treeRow{path: e.path, depth: depth, isDir: e.isDir})
			if e.isDir && t.expanded[e.path] {
				walk(e.path, depth+1)
			}
		}
	}
	walk(root, 0)
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

// treeHeader is the file column's slider header: a black "Files" bar that, when
// hovered, reveals a command menu (the Up button), matching the viewer headers.
func (s *gioUI) treeHeader(gtx C, t *tree) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &t.headerHovered, Kinds: pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			switch pe.Kind {
			case pointer.Enter:
				t.headerHovered = true
			case pointer.Leave:
				t.headerHovered = false
			}
		}
	}

	// The menu is always laid out so the header height stays constant.
	dims := layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.UniformInset(2).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &s.bUp, "Up") }),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(6), 0)} }),
					layout.Flexed(1, func(gtx C) D {
						return s.label(gtx, serifFont, font.Bold, font.Regular, 13, "Files", cedarBlack, 1)
					}),
				)
			})
		}),
	)

	// When not hovered, cover the menu with a black "Files" title bar.
	if !t.headerHovered {
		fillAt(gtx, cedarBlack, image.Rectangle{Max: dims.Size})
		cgtx := gtx
		cgtx.Constraints.Min = dims.Size
		cgtx.Constraints.Max = dims.Size
		layout.W.Layout(cgtx, func(gtx C) D {
			return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D {
				return s.label(gtx, serifFont, font.Bold, font.Regular, 13, "Files", cedarWhite, 1)
			})
		})
	}

	// Header-wide pass-through hover zone (reveals the menu without stealing clicks).
	pass := pointer.PassOp{}.Push(gtx.Ops)
	hst := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &t.headerHovered)
	hst.Pop()
	pass.Pop()

	return dims
}

func (s *gioUI) layoutTree(gtx C, t *tree) D {
	rows := t.rows()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return s.treeHeader(gtx, t) }),
		layout.Rigid(hrule),
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx C) D { return fill(gtx, cedarWhite, gtx.Constraints.Min) }),
				layout.Stacked(func(gtx C) D {
					return s.scrollList(gtx, &t.sc, len(rows), func(gtx C, i int) D {
						return s.treeRow(gtx, t, rows[i])
					})
				}),
			)
		}),
	)
}

// treeRow draws (and handles the click for) one visible row, so click handling
// is O(visible rows) rather than O(all rows).
func (s *gioUI) treeRow(gtx C, t *tree, r treeRow) D {
	c := t.click(r.path)
	if c.Clicked(gtx) {
		if r.isDir {
			t.toggle(r.path)
		} else if t.onOpen != nil {
			t.onOpen(r.path)
		}
	}
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
				if r.isDir || isTiogaName(r.path) {
					weight = font.Bold // .tioga documents stand out
				}
				return layout.Inset{Left: indent, Top: 1, Bottom: 1}.Layout(gtx, func(gtx C) D {
					return s.label(gtx, serifFont, weight, font.Regular, 13, marker+filepath.Base(r.path), cedarBlack, 1)
				})
			}),
		)
	})
}
