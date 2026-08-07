// Command tiogaviewer is a native-Go (Fyne) port of Rochus Keller's Cedar/Mesa
// TiogaViewer. It browses a Cedar source tree, decodes Tioga-formatted files,
// renders documentation natively, and shows syntax-highlighted Cedar source.
//
// Pass the root of the source tree (or a single file) as an argument, or use
// File → Open Directory (Ctrl+O) at runtime.
package main

import (
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"cedarg/internal/cedar"
	"cedarg/internal/tioga"
)

// fileSuffixes are the file kinds shown in the tree, matching the original.
var fileSuffixes = []string{"tioga", "mesa", "df", "require", "profile", "depends"}

// builtins are the identifiers rendered as types (the original viewer's list).
var builtinList = []string{
	"ATOM", "BOOL", "BOOLEAN", "CARDINAL", "CHAR", "CHARACTER", "CODE",
	"ELSE", "ISTYPE", "PACKED", "SIGNAL", "ENABLE", "JOIN", "PAINTED",
	"SIZE", "END", "LAST", "POINTER", "START", "ENDCASE", "LENGTH", "PORT",
	"STATE", "ENDLOOP", "LIST", "PRED", "STOP", "ENTRY", "LOCKS", "PRIVATE",
	"STRING", "ERROR", "LONG", "PROC", "SUCC", "EXIT", "LOOP", "PROCEDURE",
	"TEXT", "EXITS", "LOOPHOLE", "PROCESS", "THEN", "EXPORTS", "MACHINE",
	"PROGRAM", "THROUGH", "FINISHED", "MAX", "PUBLIC", "TO", "FIRST", "MIN",
	"READONLY", "TRANSFER", "FOR", "MOD", "RECORD", "TRASH", "FORK",
	"MONITOR", "REF", "TRUSTED", "FRAME", "MONITORED", "REJECT", "TYPE",
	"FREE", "NARROW", "RELATIVE", "UNCHECKED", "FROM", "NEW", "REPEAT",
	"UNCOUNTED", "GO", "NIL", "RESTART", "UNTIL", "GOTO", "NOT", "RESUME",
	"USING", "IF", "NOTIFY", "RETRY", "WAIT", "IMPORTS", "NULL", "RETURN",
	"WHILE", "IN", "OF", "RETURNS", "WITH", "INLINE", "OPEN", "SAFE",
	"ZONE", "INT", "OR", "SELECT", "INTEGER", "ORDERED", "SEQUENCE",
	"INTERNAL", "OVERLAID", "SHARES", "TRUE", "FALSE", "CARD",
}

// numColumns is the number of Cedar workspace columns.
const numColumns = 2

type ui struct {
	win  fyne.Window
	tree *widget.Tree

	colHolder  [numColumns]*fyne.Container // one stack holder per column
	columns    [numColumns][]*viewer       // viewers stacked top→bottom per column
	grown      [numColumns]*viewer         // the grown (maximised) viewer, if any
	minimized  []*viewer                   // viewers parked in the icon tray
	iconTray   *fyne.Container             // bottom strip of minimized-viewer icons
	statusText *canvas.Text                // status readout in the global bar

	rootPath   string
	builtins   map[string]bool
	childCache map[string][]string
	fontScale  float32 // theme text-size multiplier for zoom
	mono       bool    // monochrome highlighting: bold/italic, no colour
}

// viewer is one Cedar "Viewer": a document/code pane living in a column.
type viewer struct {
	ui       *ui
	path     string
	col      int              // which column it belongs to
	code     *widget.TextGrid // set for code files
	doc      *widget.RichText // set for document files
	lastCode string           // decoded source, for restyling on mono toggle
	root     fyne.CanvasObject
}

// newUI builds all widgets and the layout, and installs them as the window's
// content. It is split out from main so tests can construct the UI headlessly.
func newUI(w fyne.Window) *ui {
	u := &ui{
		win:        w,
		builtins:   make(map[string]bool, len(builtinList)),
		childCache: make(map[string][]string),
		fontScale:  1.0,
	}
	for _, b := range builtinList {
		u.builtins[b] = true
	}

	u.tree = widget.NewTree(u.childUIDs, u.isBranch, u.createNode, u.updateNode)
	u.tree.OnSelected = u.onSelected
	treePane := container.NewBorder(cedarStrip("Files"), nil, nil, nil, u.tree)

	// Two Cedar workspace columns, each a holder we refill on every reflow.
	for c := range u.colHolder {
		u.colHolder[c] = container.NewStack(emptyColumn())
	}
	workspace := container.NewHSplit(u.colHolder[0], u.colHolder[1])
	workspace.SetOffset(0.5)

	main := container.NewHSplit(treePane, workspace)
	main.SetOffset(0.18)

	u.iconTray = container.NewHBox()
	content := container.NewBorder(
		u.globalBar(),
		cedarIconTrayBar(u.iconTray),
		nil, nil,
		main,
	)
	w.SetContent(content)
	return u
}

// globalBar is the system-wide control strip across the top (black), with a few
// primary actions, like Cedar's message/command row.
func (u *ui) globalBar() fyne.CanvasObject {
	rect := canvas.NewRectangle(cedarBlack)
	rect.SetMinSize(fyne.NewSize(0, 26))
	title := canvas.NewText("Cedar  Viewers", cedarWhite)
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.TextSize = 13
	u.statusText = canvas.NewText("", cedarWhite)
	u.statusText.TextSize = 12
	open := widget.NewButton("Open…", func() { u.openFileDialog() })
	openDir := widget.NewButton("Dir…", func() { u.openDirDialog() })
	bar := container.NewBorder(nil, nil,
		container.NewHBox(container.NewPadded(title), open, openDir),
		container.NewPadded(u.statusText),
		nil)
	return container.NewStack(rect, bar)
}

// cedarStrip is a horizontal command/label strip: black text over a grey fill.
func cedarStrip(text string) fyne.CanvasObject {
	rect := canvas.NewRectangle(cedarGrey)
	lbl := canvas.NewText(text, cedarBlack)
	lbl.TextSize = 12
	return container.NewStack(rect, container.NewPadded(lbl))
}

// cedarIconTrayBar is the reserved grey bottom strip holding minimized viewers.
func cedarIconTrayBar(tray *fyne.Container) fyne.CanvasObject {
	rect := canvas.NewRectangle(cedarGreyMid)
	rect.SetMinSize(fyne.NewSize(0, 30))
	return container.NewStack(rect, container.NewHScroll(tray))
}

// emptyColumn is the placeholder shown when a column holds no viewers.
func emptyColumn() fyne.CanvasObject {
	return canvas.NewRectangle(cedarWhite)
}

func main() {
	a := app.NewWithID("ch.rochus-keller.tiogaviewer.go")
	a.Settings().SetTheme(newCedarTheme(1.0))
	w := a.NewWindow("TiogaViewer")

	u := newUI(w)
	u.buildMenu(a)

	// A command-line argument selects a source-tree root or a single file.
	// With no argument, open ./download-src if it exists (the default mirror).
	if len(os.Args) > 1 {
		if info, err := os.Stat(os.Args[1]); err == nil {
			abs, _ := filepath.Abs(os.Args[1])
			if info.IsDir() {
				u.setRoot(abs)
			} else {
				u.openFile(abs)
			}
		}
	} else if info, err := os.Stat("download-src"); err == nil && info.IsDir() {
		if abs, err := filepath.Abs("download-src"); err == nil {
			u.setRoot(abs)
		}
	}

	w.Resize(fyne.NewSize(1150, 780))
	w.ShowAndRun()
}

func (u *ui) openDirDialog() {
	dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
		if err != nil || list == nil {
			return
		}
		u.setRoot(list.Path())
	}, u.win)
}

func (u *ui) openFileDialog() {
	dialog.ShowFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		path := rc.URI().Path()
		rc.Close()
		u.openFile(path)
	}, u.win)
}

func (u *ui) buildMenu(a fyne.App) {
	openDir := u.openDirDialog
	openFile := u.openFileDialog

	zoomIn := func() { u.zoomBy(+0.1) }
	zoomOut := func() { u.zoomBy(-0.1) }
	zoomReset := func() { u.zoomReset() }

	// Primary shortcuts shown in the menu (Cmd/Super on macOS).
	miZoomIn := fyne.NewMenuItem("Zoom In", zoomIn)
	miZoomIn.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyEqual, Modifier: fyne.KeyModifierSuper}
	miZoomOut := fyne.NewMenuItem("Zoom Out", zoomOut)
	miZoomOut.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyMinus, Modifier: fyne.KeyModifierSuper}
	miZoomReset := fyne.NewMenuItem("Reset Zoom", zoomReset)
	miZoomReset.Shortcut = &desktop.CustomShortcut{KeyName: fyne.Key0, Modifier: fyne.KeyModifierSuper}

	// Monochrome toggle: bold/italic instead of colour.
	miMono := fyne.NewMenuItem("Monochrome (bold/italic)", nil)
	miMono.Checked = u.mono
	miMono.Action = func() {
		miMono.Checked = !miMono.Checked
		u.setMono(miMono.Checked)
		u.win.MainMenu().Refresh()
	}
	miMono.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyM, Modifier: fyne.KeyModifierSuper}

	u.win.SetMainMenu(fyne.NewMainMenu(
		fyne.NewMenu("File",
			fyne.NewMenuItem("Open Directory…", openDir),
			fyne.NewMenuItem("Open File…", openFile),
		),
		fyne.NewMenu("View", miZoomIn, miZoomOut, miZoomReset, fyne.NewMenuItemSeparator(), miMono),
	))
	// Cmd/Super+M is handled by the menu item above; add the Alt variant too.
	u.win.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyM, Modifier: fyne.KeyModifierAlt},
		func(fyne.Shortcut) { miMono.Action() },
	)
	u.win.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyO, Modifier: fyne.KeyModifierControl},
		func(fyne.Shortcut) { openDir() },
	)

	// Register the zoom shortcuts for both Cmd/Super and Alt, and for the "=",
	// "+" (keypad) and "-" keys, so they work regardless of layout/keypad.
	for _, mod := range []fyne.KeyModifier{fyne.KeyModifierSuper, fyne.KeyModifierAlt} {
		for _, k := range []fyne.KeyName{fyne.KeyEqual, fyne.KeyPlus} {
			u.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: k, Modifier: mod}, func(fyne.Shortcut) { zoomIn() })
		}
		u.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyMinus, Modifier: mod}, func(fyne.Shortcut) { zoomOut() })
		u.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.Key0, Modifier: mod}, func(fyne.Shortcut) { zoomReset() })
	}
}

func (u *ui) applyZoom() {
	fyne.CurrentApp().Settings().SetTheme(newCedarTheme(u.fontScale))
}

// Zoom bounds — a wide range so text can be made large for readability.
const (
	minFontScale = 0.5
	maxFontScale = 6.0
)

func (u *ui) zoomBy(delta float32) {
	u.fontScale += delta
	if u.fontScale < minFontScale {
		u.fontScale = minFontScale
	}
	if u.fontScale > maxFontScale {
		u.fontScale = maxFontScale
	}
	u.applyZoom()
}

func (u *ui) zoomReset() {
	u.fontScale = 1.0
	u.applyZoom()
}

// ---- file tree ----

func (u *ui) setRoot(path string) {
	u.rootPath = path
	u.childCache = make(map[string][]string)
	u.win.SetTitle(path + " - TiogaViewer")
	u.setStatus("Root: " + path)
	u.clearViewers()
	u.tree.Refresh()
}

// clearViewers removes all open and minimized viewers.
func (u *ui) clearViewers() {
	for c := range u.columns {
		u.columns[c] = nil
		u.grown[c] = nil
	}
	u.minimized = nil
	u.rebuildColumn(0)
	u.rebuildColumn(1)
	u.rebuildTray()
}

// relPath returns path relative to the current root, when possible.
func (u *ui) relPath(path string) string {
	if u.rootPath != "" {
		return strings.TrimPrefix(path, u.rootPath)
	}
	return path
}

// setStatus updates the readout in the global bar.
func (u *ui) setStatus(s string) {
	if u.statusText != nil {
		u.statusText.Text = s
		u.statusText.Refresh()
	}
}

func (u *ui) childUIDs(uid widget.TreeNodeID) []widget.TreeNodeID {
	dir := uid
	if uid == "" {
		dir = u.rootPath
	}
	if dir == "" {
		return nil
	}
	if c, ok := u.childCache[dir]; ok {
		return c
	}
	children := u.readDir(dir)
	u.childCache[dir] = children
	return children
}

// readDir lists sub-directories and matching files, directories first, each
// sorted by name. Empty directories are not pruned (the original did, but that
// requires a full recursive scan; lazy listing keeps a large tree responsive).
func (u *ui) readDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
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
	return append(dirs, files...)
}

func matchFile(name string) bool {
	for _, s := range fileSuffixes {
		if strings.HasSuffix(name, "."+s) || strings.Contains(name, "."+s+"!") {
			return true
		}
	}
	return false
}

func (u *ui) isBranch(uid widget.TreeNodeID) bool {
	if uid == "" {
		return true
	}
	info, err := os.Stat(uid)
	return err == nil && info.IsDir()
}

func (u *ui) createNode(bool) fyne.CanvasObject { return widget.NewLabel("") }

func (u *ui) updateNode(uid widget.TreeNodeID, _ bool, o fyne.CanvasObject) {
	o.(*widget.Label).SetText(filepath.Base(uid))
}

func (u *ui) onSelected(uid widget.TreeNodeID) {
	info, err := os.Stat(uid)
	if err != nil || info.IsDir() {
		return
	}
	u.openFile(uid)
}

// ---- file rendering ----

// openFile opens path as a new Viewer at the bottom of the shorter column, or
// focuses/restores an existing Viewer for the same file.
func (u *ui) openFile(path string) {
	if v := u.findViewer(path); v != nil {
		if u.isMinimized(v) {
			u.restoreViewer(v)
		}
		u.setStatus(u.relPath(path))
		return
	}
	c := u.shorterColumn()
	v := u.newViewer(path, c)
	u.grown[c] = nil // a new viewer needs to be visible
	u.columns[c] = append(u.columns[c], v)
	u.rebuildColumn(c)
	u.setStatus(u.relPath(path))
}

// ---- viewer lifecycle within columns ----

func (u *ui) allViewers() []*viewer {
	var all []*viewer
	for c := range u.columns {
		all = append(all, u.columns[c]...)
	}
	return append(all, u.minimized...)
}

func (u *ui) findViewer(path string) *viewer {
	for _, v := range u.allViewers() {
		if v.path == path {
			return v
		}
	}
	return nil
}

func (u *ui) isMinimized(v *viewer) bool {
	for _, m := range u.minimized {
		if m == v {
			return true
		}
	}
	return false
}

// shorterColumn returns the column with fewer active viewers (ties → column 0).
func (u *ui) shorterColumn() int {
	if len(u.columns[1]) < len(u.columns[0]) {
		return 1
	}
	return 0
}

func (u *ui) removeFromColumn(v *viewer) {
	col := u.columns[v.col]
	for i, x := range col {
		if x == v {
			u.columns[v.col] = append(col[:i], col[i+1:]...)
			break
		}
	}
	if u.grown[v.col] == v {
		u.grown[v.col] = nil
	}
}

// destroyViewer removes a viewer entirely; the column reflows to reclaim space.
func (u *ui) destroyViewer(v *viewer) {
	if u.isMinimized(v) {
		u.removeMinimized(v)
		u.rebuildTray()
		return
	}
	u.removeFromColumn(v)
	u.rebuildColumn(v.col)
}

// growViewer toggles a viewer occupying its whole column (others hidden).
func (u *ui) growViewer(v *viewer) {
	if u.grown[v.col] == v {
		u.grown[v.col] = nil
	} else {
		u.grown[v.col] = v
	}
	u.rebuildColumn(v.col)
}

// minimizeViewer parks a viewer in the icon tray.
func (u *ui) minimizeViewer(v *viewer) {
	if u.isMinimized(v) {
		return
	}
	u.removeFromColumn(v)
	u.minimized = append(u.minimized, v)
	u.rebuildColumn(v.col)
	u.rebuildTray()
}

// restoreViewer returns a minimized viewer to the bottom of its column.
func (u *ui) restoreViewer(v *viewer) {
	u.removeMinimized(v)
	u.grown[v.col] = nil
	u.columns[v.col] = append(u.columns[v.col], v)
	u.rebuildColumn(v.col)
	u.rebuildTray()
}

func (u *ui) removeMinimized(v *viewer) {
	for i, m := range u.minimized {
		if m == v {
			u.minimized = append(u.minimized[:i], u.minimized[i+1:]...)
			return
		}
	}
}

// switchViewer moves a viewer to the other column, reflowing both.
func (u *ui) switchViewer(v *viewer) {
	old := v.col
	u.removeFromColumn(v)
	v.col = (old + 1) % numColumns
	u.columns[v.col] = append(u.columns[v.col], v)
	u.rebuildColumn(old)
	u.rebuildColumn(v.col)
}

// splitViewer inserts a second Viewer of the same file directly below this one.
func (u *ui) splitViewer(v *viewer) {
	nv := u.newViewer(v.path, v.col)
	col := u.columns[v.col]
	for i, x := range col {
		if x == v {
			u.columns[v.col] = append(col[:i+1], append([]*viewer{nv}, col[i+1:]...)...)
			break
		}
	}
	u.grown[v.col] = nil
	u.rebuildColumn(v.col)
}

// rebuildColumn refills a column holder: the grown viewer alone, or all viewers
// stacked and height-partitioned via nested splits.
func (u *ui) rebuildColumn(c int) {
	var content fyne.CanvasObject
	switch {
	case u.grown[c] != nil:
		content = u.grown[c].root
	case len(u.columns[c]) > 0:
		roots := make([]fyne.CanvasObject, len(u.columns[c]))
		for i, v := range u.columns[c] {
			roots[i] = v.root
		}
		content = stackSplit(roots)
	default:
		content = emptyColumn()
	}
	u.colHolder[c].Objects = []fyne.CanvasObject{content}
	u.colHolder[c].Refresh()
}

// stackSplit stacks items top→bottom in nested vertical splits with equal
// heights, giving draggable boundaries and full-height partitioning.
func stackSplit(items []fyne.CanvasObject) fyne.CanvasObject {
	if len(items) == 1 {
		return items[0]
	}
	s := container.NewVSplit(items[0], stackSplit(items[1:]))
	s.SetOffset(1.0 / float64(len(items))) // top item gets 1/n of the height
	return s
}

// rebuildTray refills the bottom icon tray with a button per minimized viewer.
func (u *ui) rebuildTray() {
	u.iconTray.Objects = nil
	for _, v := range u.minimized {
		mv := v
		b := widget.NewButton(filepath.Base(v.path), func() { u.restoreViewer(mv) })
		u.iconTray.Add(b)
	}
	u.iconTray.Refresh()
}

// newViewer reads path, decodes it, and builds a bordered Viewer with a Cedar
// header (title + Destroy/Grow/Icon/Switch/Split) over the rendered content.
func (u *ui) newViewer(path string, col int) *viewer {
	v := &viewer{ui: u, path: path, col: col}
	rel := u.relPath(path)
	data, err := os.ReadFile(path)

	var content fyne.CanvasObject
	switch {
	case err != nil:
		content = widget.NewLabel("cannot open file: " + rel)
	case strings.HasSuffix(path, ".mesa") || strings.Contains(path, ".mesa!"):
		doc := tioga.Read(data, true)
		v.code = widget.NewTextGrid()
		v.lastCode = doc.Code
		v.code.SetText(doc.Code)
		v.styleCode()
		content = container.NewScroll(v.code)
	default:
		doc := tioga.Read(data, false)
		v.doc = widget.NewRichText()
		v.doc.Wrapping = fyne.TextWrapWord
		segs := make([]widget.RichTextSegment, 0, len(doc.Blocks))
		for _, b := range doc.Blocks {
			segs = append(segs, blockSegment(b))
		}
		v.doc.Segments = segs
		content = container.NewScroll(v.doc)
	}

	title := widget.NewLabel(rel)
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Truncation = fyne.TextTruncateEllipsis

	// Header action buttons, as on a Cedar Viewer.
	buttons := container.NewHBox(
		hdrButton("Destroy", func() { u.destroyViewer(v) }),
		hdrButton("Grow", func() { u.growViewer(v) }),
		hdrButton("Icon", func() { u.minimizeViewer(v) }),
		hdrButton("Switch", func() { u.switchViewer(v) }),
		hdrButton("Split", func() { u.splitViewer(v) }),
	)
	header := container.NewVBox(
		container.NewStack(canvas.NewRectangle(cedarGrey),
			container.NewBorder(nil, nil, buttons, nil, title)),
		widget.NewSeparator(),
	)

	inner := container.NewBorder(header, nil, nil, nil, content)
	v.root = tileFrame(inner)
	return v
}

// hdrButton is a compact, low-importance header action button.
func hdrButton(label string, tapped func()) *widget.Button {
	b := widget.NewButton(label, tapped)
	b.Importance = widget.LowImportance
	return b
}

// styleCode applies syntax styling to the Viewer's TextGrid, in colour or
// monochrome (bold/italic) mode.
func (v *viewer) styleCode() {
	if v.code == nil {
		return
	}
	for _, s := range cedar.Highlight(v.lastCode, v.ui.builtins) {
		var st cedar.Style
		if v.ui.mono {
			st = cedar.CategoryStyleMono(s.Cat)
		} else {
			st = cedar.CategoryStyle(s.Cat)
		}
		style := &widget.CustomTextGridStyle{}
		if st.HasFG {
			style.FGColor = color.Color(st.FG)
		}
		if st.HasBG {
			style.BGColor = color.Color(st.BG)
		}
		style.TextStyle = fyne.TextStyle{Bold: st.Bold, Italic: st.Italic}
		v.code.SetStyleRange(s.Row, s.Col, s.Row, s.Col+s.Len-1, style)
	}
	v.code.Refresh()
}

// setMono switches highlighting mode and restyles every open code Viewer.
func (u *ui) setMono(on bool) {
	u.mono = on
	for _, v := range u.allViewers() {
		if v.code != nil {
			v.code.SetText(v.lastCode) // clears old per-cell styles
			v.styleCode()
		}
	}
}

// tileFrame wraps content in a 1px black border, like a Cedar viewer edge.
func tileFrame(content fyne.CanvasObject) fyne.CanvasObject {
	border := canvas.NewRectangle(cedarWhite)
	border.StrokeColor = cedarBlack
	border.StrokeWidth = 1
	return container.NewStack(border, content)
}

// Block-level variants of Fyne's heading/quote styles. The predefined styles
// are Inline (they flow into neighbouring text); forcing Inline=false makes each
// Tioga block start on its own line, so the document's line structure is kept.
var (
	styleHeading    = asBlock(widget.RichTextStyleHeading)
	styleSubHeading = asBlock(widget.RichTextStyleSubHeading)
	styleQuote      = asBlock(widget.RichTextStyleBlockquote)
)

func asBlock(s widget.RichTextStyle) widget.RichTextStyle {
	s.Inline = false
	return s
}

func blockSegment(b tioga.Block) widget.RichTextSegment {
	switch b.Kind {
	case tioga.Heading:
		style := styleHeading
		if b.Level >= 2 {
			style = styleSubHeading
		}
		return &widget.TextSegment{Text: b.Text, Style: style}
	case tioga.Code:
		return &widget.TextSegment{Text: b.Text, Style: widget.RichTextStyleCodeBlock}
	case tioga.Quote:
		return &widget.TextSegment{Text: b.Text, Style: styleQuote}
	default:
		return &widget.TextSegment{Text: b.Text, Style: widget.RichTextStyleParagraph}
	}
}
