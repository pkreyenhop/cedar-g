package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"cedarg/internal/tioga"
)

const (
	numColumns  = 2
	dividerDp   = 6
	minViewerDp = 44
)

// column holds viewers stacked top→bottom with proportional weights.
type column struct {
	viewers []*viewer
	weights []float32 // len == len(viewers), sums to 1
	grown   *viewer   // maximised within the column, if any

	dragActive   bool
	dragBoundary int // boundary index between viewers[i] and viewers[i+1]
}

func newColumn() *column { return &column{} }

// ---- lifecycle ----

func (s *gioUI) allViewers() []*viewer {
	var all []*viewer
	for _, c := range s.cols {
		all = append(all, c.viewers...)
	}
	return append(all, s.minimized...)
}

func (s *gioUI) findViewer(path string) *viewer {
	for _, v := range s.allViewers() {
		if v.path == path {
			return v
		}
	}
	return nil
}

func (s *gioUI) isMinimized(v *viewer) bool {
	for _, m := range s.minimized {
		if m == v {
			return true
		}
	}
	return false
}

func (s *gioUI) shorterColumn() int {
	if len(s.cols[1].viewers) < len(s.cols[0].viewers) {
		return 1
	}
	return 0
}

func (s *gioUI) openFile(path string) {
	if v := s.findViewer(path); v != nil {
		if s.isMinimized(v) {
			s.restoreViewer(v)
		}
		return
	}
	s.addViewer(s.newViewer(path))
}

// addViewer places a freshly-created viewer at the bottom of the shorter column.
func (s *gioUI) addViewer(v *viewer) {
	c := s.shorterColumn()
	s.cols[c].grown = nil
	s.addToColumn(c, v)
}

// openTerminal opens a shell in a new viewer.
func (s *gioUI) openTerminal() { s.addViewer(s.newTerminalViewer()) }

// openNewDocument opens a blank editable document in a new viewer.
func (s *gioUI) openNewDocument() { s.addViewer(s.newEditorViewer()) }

// addToColumn appends v, shrinking existing viewers proportionally.
func (s *gioUI) addToColumn(c int, v *viewer) {
	col := s.cols[c]
	n := len(col.viewers) + 1
	scale := float32(n-1) / float32(n)
	for i := range col.weights {
		col.weights[i] *= scale
	}
	v.col = c
	col.viewers = append(col.viewers, v)
	col.weights = append(col.weights, 1/float32(n))
}

func indexOfViewer(list []*viewer, v *viewer) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}

// removeFromColumn drops v and renormalises the remaining weights to sum to 1.
func (s *gioUI) removeFromColumn(v *viewer) {
	col := s.cols[v.col]
	i := indexOfViewer(col.viewers, v)
	if i < 0 {
		return
	}
	w := col.weights[i]
	col.viewers = append(col.viewers[:i], col.viewers[i+1:]...)
	col.weights = append(col.weights[:i], col.weights[i+1:]...)
	if col.grown == v {
		col.grown = nil
	}
	if rem := 1 - w; rem > 0 {
		for j := range col.weights {
			col.weights[j] /= rem
		}
	}
}

func (s *gioUI) removeMinimized(v *viewer) {
	for i, m := range s.minimized {
		if m == v {
			s.minimized = append(s.minimized[:i], s.minimized[i+1:]...)
			return
		}
	}
}

func (s *gioUI) destroyViewer(v *viewer) {
	if v.term != nil {
		v.term.close()
	}
	if s.isMinimized(v) {
		s.removeMinimized(v)
		return
	}
	s.removeFromColumn(v)
}

func (s *gioUI) growViewer(v *viewer) {
	col := s.cols[v.col]
	if col.grown == v {
		col.grown = nil
	} else {
		col.grown = v
	}
}

func (s *gioUI) minimizeViewer(v *viewer) {
	if s.isMinimized(v) {
		return
	}
	s.removeFromColumn(v)
	s.minimized = append(s.minimized, v)
}

func (s *gioUI) restoreViewer(v *viewer) {
	s.removeMinimized(v)
	s.addToColumn(v.col, v)
}

func (s *gioUI) switchViewer(v *viewer) {
	old := v.col
	s.removeFromColumn(v)
	s.addToColumn((old+1)%numColumns, v)
}

// splitViewer inserts a sibling of the same file directly below v, sharing its
// height.
func (s *gioUI) splitViewer(v *viewer) {
	if v.kind != vkContent {
		return // only file viewers can be split
	}
	col := s.cols[v.col]
	i := indexOfViewer(col.viewers, v)
	if i < 0 {
		return
	}
	nv := s.newViewer(v.path)
	nv.col = v.col
	half := col.weights[i] / 2
	col.weights[i] = half
	col.viewers = append(col.viewers[:i+1], append([]*viewer{nv}, col.viewers[i+1:]...)...)
	col.weights = append(col.weights[:i+1], append([]float32{half}, col.weights[i+1:]...)...)
	col.grown = nil
}

// processHeaderActions handles every viewer's header buttons and tray restores.
func (s *gioUI) processHeaderActions(gtx C) {
	for _, v := range s.allViewers() {
		if v.bDestroy.Clicked(gtx) {
			s.destroyViewer(v)
			return // slice changed; re-evaluate next frame
		}
		if v.bGrow.Clicked(gtx) {
			s.growViewer(v)
		}
		if v.bIcon.Clicked(gtx) {
			s.minimizeViewer(v)
			return
		}
		if v.bSwitch.Clicked(gtx) {
			s.switchViewer(v)
			return
		}
		if v.bSplit.Clicked(gtx) {
			s.splitViewer(v)
			return
		}
		if v.bRestore.Clicked(gtx) {
			s.restoreViewer(v)
			return
		}
		if v.kind == vkEditor && v.bSave.Clicked(gtx) {
			s.saveEditor(v)
		}
		if v.runnable && v.bRun.Clicked(gtx) {
			s.runViewer(v)
			return // added a viewer; re-evaluate next frame
		}
	}
}

// runViewer evaluates a viewer's Mesa source — the editor buffer for a scratch
// document, or the decoded file source for a .mesa viewer — and shows the
// output in a companion viewer (reused across runs).
func (s *gioUI) runViewer(v *viewer) {
	name := strings.TrimSpace(v.nameEd.Text())
	if name == "" {
		name = v.headerTitle()
	}
	title := "Run: " + name
	code := v.src // a .mesa content viewer runs its decoded source
	if v.kind == vkEditor {
		code = v.editor.Text()
	}
	out := runMesa(code)
	if v.runOut != nil && s.viewerAlive(v.runOut) {
		v.runOut.title = title
		v.runOut.outText = out
	} else {
		v.runOut = s.newOutputViewer(title, out)
		s.addViewer(v.runOut)
	}
	if s.invalidate != nil {
		s.invalidate()
	}
}

// viewerAlive reports whether v is still on screen (in a column or minimized).
func (s *gioUI) viewerAlive(v *viewer) bool {
	for _, x := range s.allViewers() {
		if x == v {
			return true
		}
	}
	return false
}

// saveEditor encodes the editor's text as a Tioga file and writes it to disk.
func (s *gioUI) saveEditor(v *viewer) {
	name := strings.TrimSpace(v.nameEd.Text())
	if name == "" {
		name = "Untitled.tioga"
	}
	// Tioga scratch buffers are encoded and default to a .tioga extension; plain
	// text files keep whatever name (and extension) they already have.
	if !v.plainText && !strings.HasSuffix(name, ".tioga") {
		name += ".tioga"
	}
	dir := s.root
	if dir == "" {
		dir, _ = os.Getwd()
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, name)
	}
	data := tioga.Encode(v.editor.Text())
	if v.plainText {
		data = []byte(v.editor.Text())
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		v.saveMsg = "save failed: " + err.Error()
		return
	}
	v.path = path
	v.rel = s.relPath(path)
	v.title = filepath.Base(path)
	v.saveMsg = "saved " + v.rel
	s.tree.refresh() // reveal the new file in the tree
	if s.invalidate != nil {
		s.invalidate()
	}
}

// ---- layout ----

func (s *gioUI) layoutColumn(gtx C, c int) D {
	col := s.cols[c]
	gtx.Constraints.Min = gtx.Constraints.Max
	fill(gtx, cedarWhite, gtx.Constraints.Min)

	if col.grown != nil {
		return s.layoutViewer(gtx, col.grown)
	}
	n := len(col.viewers)
	if n == 0 {
		return D{Size: gtx.Constraints.Max}
	}

	H := gtx.Constraints.Max.Y
	dividerH := gtx.Dp(dividerDp)
	s.processColumnDrag(gtx, col, H, dividerH)
	avail := H - (n-1)*dividerH

	children := make([]layout.FlexChild, 0, 2*n-1)
	for i := 0; i < n; i++ {
		v := col.viewers[i]
		if i == n-1 {
			children = append(children, layout.Flexed(1, func(gtx C) D { return s.layoutViewer(gtx, v) }))
		} else {
			hi := int(col.weights[i] * float32(avail))
			children = append(children, layout.Rigid(func(gtx C) D {
				return sized(gtx, gtx.Constraints.Max.X, hi, func(gtx C) D { return s.layoutViewer(gtx, v) })
			}))
			children = append(children, layout.Rigid(func(gtx C) D { return s.divider(gtx, dividerH) }))
		}
	}

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D {
			sz := gtx.Constraints.Min
			defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, col)
			pointer.CursorRowResize.Add(gtx.Ops)
			return D{Size: sz}
		}),
		layout.Stacked(func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		}),
	)
}

func (s *gioUI) divider(gtx C, h int) D {
	sz := image.Pt(gtx.Constraints.Max.X, h)
	fill(gtx, cedarGreyMid, sz)
	fillAt(gtx, cedarBlack, image.Rect(0, 0, sz.X, 1))
	fillAt(gtx, cedarBlack, image.Rect(0, h-1, sz.X, h))
	return D{Size: sz}
}

// vdivider is the vertical counterpart of divider: a grey grab-bar with black
// edges between two horizontally-tiled panes.
func (s *gioUI) vdivider(gtx C, w int) D {
	sz := image.Pt(w, gtx.Constraints.Max.Y)
	fill(gtx, cedarGreyMid, sz)
	fillAt(gtx, cedarBlack, image.Rect(0, 0, 1, sz.Y))
	fillAt(gtx, cedarBlack, image.Rect(w-1, 0, w, sz.Y))
	return D{Size: sz}
}

// hsplitW is like hsplit but the left pane has a fixed width in dp (kept small
// even on wide displays), adjusted by dragging the divider.
func (s *gioUI) hsplitW(gtx C, widthDp *float32, active *bool, left, right layout.Widget) D {
	dividerW := gtx.Dp(dividerDp)
	leftW := gtx.Dp(unit.Dp(*widthDp))
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: widthDp, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			if abs(int(pe.Position.X)-leftW) <= gtx.Dp(8) {
				*active = true
			}
		case pointer.Drag:
			if *active {
				*widthDp = clampf(float32(gtx.Metric.PxToDp(int(pe.Position.X))), 120, 520)
				leftW = gtx.Dp(unit.Dp(*widthDp))
			}
		case pointer.Release, pointer.Cancel:
			*active = false
		}
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D {
			sz := gtx.Constraints.Min
			defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, widthDp)
			pointer.CursorColResize.Add(gtx.Ops)
			return D{Size: sz}
		}),
		layout.Stacked(func(gtx C) D {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return sized(gtx, leftW, gtx.Constraints.Max.Y, left)
				}),
				layout.Rigid(func(gtx C) D { return s.vdivider(gtx, dividerW) }),
				layout.Flexed(1, right),
			)
		}),
	)
}

// hsplit lays out left | divider | right with a draggable vertical divider that
// adjusts *frac (left's fraction of the width). The pointer input covers the
// whole area with a fixed origin, so dragging is stable as the boundary moves;
// content inputs (lists, buttons) sit on top and are unaffected.
func (s *gioUI) hsplit(gtx C, frac *float32, active *bool, left, right layout.Widget) D {
	W := gtx.Constraints.Max.X
	dividerW := gtx.Dp(dividerDp)
	divX := int(*frac * float32(W))
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: frac, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			if abs(int(pe.Position.X)-divX) <= gtx.Dp(8) {
				*active = true
			}
		case pointer.Drag:
			if *active {
				*frac = clampf(pe.Position.X/float32(W), 0.12, 0.88)
			}
		case pointer.Release, pointer.Cancel:
			*active = false
		}
	}

	leftW := int(*frac * float32(W-dividerW))
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D {
			sz := gtx.Constraints.Min
			defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, frac)
			pointer.CursorColResize.Add(gtx.Ops)
			return D{Size: sz}
		}),
		layout.Stacked(func(gtx C) D {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return sized(gtx, leftW, gtx.Constraints.Max.Y, left)
				}),
				layout.Rigid(func(gtx C) D { return s.vdivider(gtx, dividerW) }),
				layout.Flexed(1, right),
			)
		}),
	)
}

// processColumnDrag adjusts weights when a boundary is dragged.
func (s *gioUI) processColumnDrag(gtx C, col *column, H, dividerH int) {
	n := len(col.viewers)
	if n < 2 {
		return
	}
	avail := H - (n-1)*dividerH
	minPx := gtx.Dp(minViewerDp)
	heights := make([]int, n)
	for i := range heights {
		heights[i] = int(col.weights[i] * float32(avail))
	}
	// pixel of the top of boundary b (the divider after viewer b)
	dividerTop := func(b int) int {
		y := 0
		for i := 0; i <= b; i++ {
			y += heights[i]
		}
		return y + b*dividerH
	}

	for {
		ev, ok := gtx.Event(pointer.Filter{Target: col, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			py := int(pe.Position.Y)
			col.dragActive = false
			for b := 0; b < n-1; b++ {
				if abs(py-(dividerTop(b)+dividerH/2)) <= gtx.Dp(8) {
					col.dragActive = true
					col.dragBoundary = b
					break
				}
			}
		case pointer.Drag:
			if col.dragActive {
				b := col.dragBoundary
				topPair := 0
				for i := 0; i < b; i++ {
					topPair += heights[i]
				}
				topPair += b * dividerH
				pairH := heights[b] + heights[b+1]
				newTop := int(pe.Position.Y) - topPair
				if newTop < minPx {
					newTop = minPx
				}
				if newTop > pairH-minPx {
					newTop = pairH - minPx
				}
				heights[b] = newTop
				heights[b+1] = pairH - newTop
				if avail > 0 {
					col.weights[b] = float32(heights[b]) / float32(avail)
					col.weights[b+1] = float32(heights[b+1]) / float32(avail)
				}
			}
		case pointer.Release, pointer.Cancel:
			col.dragActive = false
		}
	}
}

// layoutIconTray draws the reserved bottom strip of minimized-viewer icons.
func (s *gioUI) layoutIconTray(gtx C) D {
	h := gtx.Dp(30)
	gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGreyMid, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			children := make([]layout.FlexChild, 0, len(s.minimized))
			for _, v := range s.minimized {
				vv := v
				children = append(children, layout.Rigid(func(gtx C) D {
					return layout.UniformInset(3).Layout(gtx, func(gtx C) D {
						return s.flatButton(gtx, &vv.bRestore, filepath.Base(vv.path))
					})
				}))
			}
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
		}),
	)
}
