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
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
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

type ui struct {
	win   fyne.Window
	tree  *widget.Tree
	title *widget.Label

	doc        *widget.RichText
	docScroll  *container.Scroll
	code       *widget.TextGrid
	codeScroll *container.Scroll

	rootPath   string
	builtins   map[string]bool
	childCache map[string][]string
	fontScale  float32 // theme text-size multiplier for zoom
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

	u.title = widget.NewLabel("")
	u.title.Wrapping = fyne.TextWrapWord

	u.doc = widget.NewRichText()
	u.doc.Wrapping = fyne.TextWrapWord
	u.docScroll = container.NewScroll(u.doc)

	u.code = widget.NewTextGrid()
	u.codeScroll = container.NewScroll(u.code)
	u.codeScroll.Hide()

	viewers := container.NewStack(u.docScroll, u.codeScroll)

	u.tree = widget.NewTree(u.childUIDs, u.isBranch, u.createNode, u.updateNode)
	u.tree.OnSelected = u.onSelected

	right := container.NewBorder(u.title, nil, nil, nil, viewers)
	split := container.NewHSplit(u.tree, right)
	split.SetOffset(0.28)
	w.SetContent(split)
	return u
}

func main() {
	a := app.NewWithID("ch.rochus-keller.tiogaviewer.go")
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

func (u *ui) buildMenu(a fyne.App) {
	openDir := func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			u.setRoot(list.Path())
		}, u.win)
	}
	openFile := func() {
		dialog.ShowFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			path := rc.URI().Path()
			rc.Close()
			u.openFile(path)
		}, u.win)
	}

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

	u.win.SetMainMenu(fyne.NewMainMenu(
		fyne.NewMenu("File",
			fyne.NewMenuItem("Open Directory…", openDir),
			fyne.NewMenuItem("Open File…", openFile),
		),
		fyne.NewMenu("View", miZoomIn, miZoomOut, miZoomReset),
	))
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

// zoomTheme wraps a base theme and scales every size by a factor, so changing it
// resizes all text (both viewers, tree and menus) as a whole-UI zoom.
type zoomTheme struct {
	base  fyne.Theme
	scale float32
}

func (z *zoomTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	return z.base.Color(n, v)
}
func (z *zoomTheme) Font(s fyne.TextStyle) fyne.Resource     { return z.base.Font(s) }
func (z *zoomTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return z.base.Icon(n) }
func (z *zoomTheme) Size(n fyne.ThemeSizeName) float32       { return z.base.Size(n) * z.scale }

func (u *ui) applyZoom() {
	fyne.CurrentApp().Settings().SetTheme(&zoomTheme{base: theme.DefaultTheme(), scale: u.fontScale})
}

func (u *ui) zoomBy(delta float32) {
	u.fontScale += delta
	if u.fontScale < 0.6 {
		u.fontScale = 0.6
	}
	if u.fontScale > 3.0 {
		u.fontScale = 3.0
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
	u.title.SetText("")
	u.doc.Segments = nil
	u.doc.Refresh()
	u.tree.Refresh()
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

func (u *ui) openFile(path string) {
	data, err := os.ReadFile(path)
	rel := path
	if u.rootPath != "" {
		rel = strings.TrimPrefix(path, u.rootPath)
	}
	if err != nil {
		u.title.SetText("cannot open file: " + rel)
		return
	}
	u.title.SetText(rel)

	isCode := strings.HasSuffix(path, ".mesa") || strings.Contains(path, ".mesa!")
	docModel := tioga.Read(data, isCode)
	if docModel.IsCode {
		u.showCode(docModel.Code)
	} else {
		u.showDoc(docModel.Blocks)
	}
}

func (u *ui) showCode(text string) {
	u.code.SetText(text)
	for _, s := range cedar.Highlight(text, u.builtins) {
		st := cedar.CategoryStyle(s.Cat)
		style := &widget.CustomTextGridStyle{}
		if st.HasFG {
			style.FGColor = color.Color(st.FG)
		}
		if st.HasBG {
			style.BGColor = color.Color(st.BG)
		}
		if st.Bold {
			style.TextStyle = fyne.TextStyle{Bold: true}
		}
		u.code.SetStyleRange(s.Row, s.Col, s.Row, s.Col+s.Len-1, style)
	}
	u.code.Refresh()
	u.docScroll.Hide()
	u.codeScroll.Show()
	u.codeScroll.ScrollToTop()
}

func (u *ui) showDoc(blocks []tioga.Block) {
	segs := make([]widget.RichTextSegment, 0, len(blocks))
	for _, b := range blocks {
		segs = append(segs, blockSegment(b))
	}
	u.doc.Segments = segs
	u.doc.Refresh()
	u.codeScroll.Hide()
	u.docScroll.Show()
	u.docScroll.ScrollToTop()
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
