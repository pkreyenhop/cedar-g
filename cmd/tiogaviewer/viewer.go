package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"cedarg/internal/cedar"
	"cedarg/internal/gargoyle"
	"cedarg/internal/tioga"
)

// builtinList are identifiers rendered as types (the original viewer's list).
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

// codeTextSize is the default code font size (larger than the doc body for
// readability; scaled further by zoom).
const codeTextSize = 17
const termTextSize = 17

// blinkHalf is the on/off half-period of the terminal caret.
const blinkHalf = 500 * time.Millisecond

// run is a styled slice of a code line.
type run struct {
	text string
	cat  cedar.Category
}

type viewerKind int

const (
	vkContent   viewerKind = iota // a decoded document or code file
	vkEditor                      // a blank/editable Tioga document
	vkTerm                        // a shell terminal
	vkOutput                      // read-only program output (e.g. a Mesa run)
	vkCommander                   // a Cedar command interpreter (CommandTool)
)

// viewer is one Cedar "Viewer": a pane living in a column.
type viewer struct {
	kind     viewerKind
	path     string
	rel      string
	title    string // header title override (editor/terminal)
	col      int
	isCode   bool
	lines    [][]run                   // code
	blocks   []tioga.Block             // documents
	root     *tioga.Node               // documents: the full node tree
	artCache map[*byte]*gargoyle.Scene // parsed artwork scenes, by Art bytes
	editor   widget.Editor             // vkEditor
	term     *terminal                 // vkTerm
	sc       scroller

	bDestroy, bGrow, bIcon, bSwitch, bSplit widget.Clickable
	bRestore                                widget.Clickable // in the icon tray
	headerHovered                           bool
	termFocus                               int // key/pointer focus tag for the terminal

	bSave      widget.Clickable // editor: save to disk
	bRun       widget.Clickable // run as Mesa
	bEdit      widget.Clickable // code viewer: toggle edit/view
	nameEd     widget.Editor    // editor: target filename
	saveMsg    string           // editor: last save status
	runOut     *viewer          // its output viewer, reused across runs
	plainText  bool             // editor: save raw text (else encode as Tioga)
	codeEdit   bool             // editor: use a monospace face
	runnable   bool             // show the Run (Mesa) button
	src        string           // vkContent .mesa: decoded source, for running
	editing    bool             // code viewer: editing (vs highlighted view)
	editorInit bool             // code viewer: the edit buffer has been loaded

	outText string // vkOutput: the captured program output

	ptrPos f32.Point // bull's-eye cursor position
	ptrIn  bool      // pointer is over the content

	selBlock    int                // selected block in the read-only view (-1 none)
	blockClicks []widget.Clickable // per-visible-block click targets

	// Right-button pop-up menu.
	menuOpen                                         bool
	menuPos                                          f32.Point
	bmLevels, bmFind, bmRefs, bmEdit, bmPrint, bmArt widget.Clickable

	cmdLog []string      // vkCommander: the command/output log
	cmdEd  widget.Editor // vkCommander: the command input line

	// Levels: an outline view that shows only nodes up to a nesting depth.
	bLevels                                 widget.Clickable // header: reveal the levels bar
	showLevels                              bool
	levelCap                                int // 0 = all levels; else show Depth <= levelCap
	bLvlFirst, bLvlFewer, bLvlMore, bLvlAll widget.Clickable

	// Find: incremental search over the (level-filtered) blocks.
	bFind                widget.Clickable
	findOpen             bool
	findEd               widget.Editor
	findQuery            string
	findMatches          []int // indices into the visible blocks
	findIdx              int
	bFindNext, bFindPrev widget.Clickable

	// Cross-references and export.
	bRefs, bPrint widget.Clickable
	showRefs      bool
	refs          []*docRef // file references found in the document
	pendingRef    string    // an inline link clicked this frame

	// Structure editing: edit the node tree (nest/unnest, insert, looks).
	bStruct                                  widget.Clickable // header: toggle structure editing
	structEdit                               bool
	doc                                      *tioga.Doc                     // the mutable tree (== root, wrapped)
	sel                                      *tioga.Node                    // selected node
	focusNode                                *tioga.Node                    // request keyboard focus here next frame
	nodeEds                                  map[*tioga.Node]*widget.Editor // per-node text editors
	bNewSib, bNewChild, bNest, bUnnest, bDel widget.Clickable
	bBold, bItalic, bStructSave              widget.Clickable
	bUndo, bRedo                             widget.Clickable
	undoStack, redoStack                     []*tioga.Node // tree snapshots

	// EditTool: a Cedar-style panel to Get/Set the node format and apply looks.
	bEditTool                          widget.Clickable
	showEditTool                       bool
	formatEd                           widget.Editor
	fmtSel                             *tioga.Node // node whose format is loaded in formatEd
	bFmtSet, bFmtGet                   widget.Clickable
	bLkU, bLkE, bLkK, bLkX, bLkH, bLkL widget.Clickable
}

// editableExts are the plain-text file formats opened in an editable viewer.
var editableExts = map[string]bool{".txt": true, ".md": true, ".go": true, ".c": true}

// fileExt returns a path's lower-case extension, ignoring any Cedar "!N"
// version suffix.
func fileExt(path string) string {
	b := filepath.Base(path)
	if i := strings.IndexByte(b, '!'); i >= 0 {
		b = b[:i]
	}
	return strings.ToLower(filepath.Ext(b))
}

func (v *viewer) headerTitle() string {
	if v.kind == vkTerm && v.term != nil {
		if d := v.term.dir(); d != "" {
			return d
		}
		return "Terminal"
	}
	if v.title != "" {
		return v.title
	}
	return v.rel
}

func (s *gioUI) newViewer(path string) *viewer {
	data, err := os.ReadFile(path)
	ext := fileExt(path)
	if err == nil && editableExts[ext] {
		return s.newTextEditorViewer(path, string(data), ext)
	}
	v := &viewer{path: path, rel: s.relPath(path)}
	if err != nil {
		v.blocks = []tioga.Block{{Kind: tioga.Paragraph, Text: "cannot open: " + v.rel}}
		v.selBlock = -1
		return v
	}
	// A .mesa file is always code. Other Tioga files (e.g. a .tioga saved by the
	// editor) are shown as code too when their content is a Mesa module, so the
	// syntax highlighter applies; genuine prose documents fall through.
	code := tioga.Read(data, true).Code
	if ext == ".mesa" || looksLikeMesa(code) {
		v.isCode = true
		v.src = code // keep the source so the Run button can execute it
		v.runnable = true
		v.lines = codeToRuns(expandTabs(code), s.builtins)
		return v
	}
	doc := tioga.Read(data, false)
	// Join a table split across header / data / TOTAL blocks into one grid, then
	// keep tabs on table blocks (the grid renderer needs them) while expanding
	// tabs to spaces elsewhere so Gio does not draw missing-glyph boxes.
	doc.Blocks = mergeTableBlocks(doc.Blocks)
	for i := range doc.Blocks {
		if !looksLikeTable(doc.Blocks[i]) {
			doc.Blocks[i].Text = expandTabs(doc.Blocks[i].Text)
		}
	}
	v.blocks = doc.Blocks
	v.root = doc.Root
	v.selBlock = -1
	return v
}

// isDoc reports whether v is a rendered prose document (not code, terminal,
// editor or output) — the viewers the Levels outline applies to.
func (v *viewer) isDoc() bool {
	return v.kind == vkContent && !v.isCode && len(v.blocks) > 0
}

// maxBlockDepth is the deepest nesting among the document's blocks.
func (v *viewer) maxBlockDepth() int {
	m := 1
	for _, b := range v.blocks {
		if b.Depth > m {
			m = b.Depth
		}
	}
	return m
}

// mesaModuleRe matches a Mesa/Cedar module declaration header, e.g.
// "Foo: CEDAR PROGRAM" or "Bar: DEFINITIONS".
var mesaModuleRe = regexp.MustCompile(
	`(?m)^\s*\w+\s*:\s*(PUBLIC\s+|PRIVATE\s+)?(CEDAR\s+|MACHINE\s+DEPENDENT\s+)?` +
		`(PROGRAM|DEFINITIONS|MONITOR|MODULE|IMPLEMENTATION)\b`)

// looksLikeMesa reports whether decoded text is a Mesa/Cedar source module,
// distinguishing editor-saved code from prose Tioga documents.
func looksLikeMesa(code string) bool {
	return mesaModuleRe.MatchString(code)
}

// newTextEditorViewer opens an existing plain-text file (txt/md/go/c) for
// editing. Saving writes raw text; code files use a monospace face. Tabs are
// expanded to spaces on load because Gio's editor cannot render tab stops; a
// consequence is that saved source uses spaces rather than tabs.
func (s *gioUI) newTextEditorViewer(path, content, ext string) *viewer {
	v := &viewer{
		kind:      vkEditor,
		path:      path,
		rel:       s.relPath(path),
		title:     filepath.Base(path),
		plainText: true,
		codeEdit:  ext == ".go" || ext == ".c",
	}
	v.editor.SetText(expandTabs(content))
	v.nameEd.SetText(filepath.Base(path))
	return v
}

var newDocCount int

// newEditorViewer opens a blank editable document. It is a scratch buffer that
// saves as Tioga and can be run as Mesa.
func (s *gioUI) newEditorViewer() *viewer {
	newDocCount++
	v := &viewer{kind: vkEditor, title: fmt.Sprintf("New Document %d", newDocCount), runnable: true}
	v.nameEd.SetText(fmt.Sprintf("Untitled-%d.tioga", newDocCount))
	return v
}

// newTerminalViewer opens a shell terminal.
func (s *gioUI) newTerminalViewer() *viewer {
	return &viewer{kind: vkTerm, title: "Terminal", term: newTerminal(s.invalidate)}
}

// newOutputViewer shows read-only program output.
func (s *gioUI) newOutputViewer(title, text string) *viewer {
	return &viewer{kind: vkOutput, title: title, outText: text}
}

// layoutViewer draws one bordered Viewer: header (actions + title), rule, body.
func (s *gioUI) layoutViewer(gtx C, v *viewer) D {
	return bordered(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return s.header(gtx, v) }),
			layout.Rigid(hrule),
			layout.Flexed(1, func(gtx C) D {
				// The bull's-eye cursor stands in over reading content (documents and
				// code); editors, terminals and the commander keep the text cursor.
				if v.kind == vkContent {
					return s.withBullseye(gtx, v, func(gtx C) D { return s.body(gtx, v) })
				}
				return s.body(gtx, v)
			}),
		)
	})
}

func (s *gioUI) header(gtx C, v *viewer) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	// Header hover state (from the pass-through zone registered last frame).
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.headerHovered, Kinds: pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			switch pe.Kind {
			case pointer.Enter:
				v.headerHovered = true
			case pointer.Leave:
				v.headerHovered = false
			}
		}
	}

	// The command menu (action buttons + title) is always laid out, so the header
	// height is the same whether the black title bar or the menu is shown.
	dims := layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.UniformInset(2).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bDestroy, "Destroy") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bGrow, "Grow") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bIcon, "Icon") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bSwitch, "Switch") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bSplit, "Split") }),
					layout.Rigid(func(gtx C) D {
						if v.kind != vkContent || !v.runnable {
							return D{}
						}
						return s.flatButton(gtx, &v.bRun, "Run") // run a .mesa file
					}),
					layout.Rigid(func(gtx C) D {
						if v.kind != vkContent || !v.isCode {
							return D{}
						}
						label := "Edit"
						if v.editing {
							label = "View"
						}
						return s.flatButton(gtx, &v.bEdit, label)
					}),
					layout.Rigid(func(gtx C) D {
						if v.kind != vkContent || !v.editing {
							return D{}
						}
						return s.flatButton(gtx, &v.bSave, "Save")
					}),
					layout.Rigid(func(gtx C) D {
						if !v.isDoc() {
							return D{}
						}
						return s.flatButton(gtx, &v.bLevels, "Levels") // outline view
					}),
					layout.Rigid(func(gtx C) D {
						if !v.isDoc() {
							return D{}
						}
						return s.flatButton(gtx, &v.bFind, "Find") // incremental search
					}),
					layout.Rigid(func(gtx C) D {
						if !v.isDoc() {
							return D{}
						}
						return s.flatButton(gtx, &v.bRefs, "Refs") // cross-references
					}),
					layout.Rigid(func(gtx C) D {
						if !v.isDoc() {
							return D{}
						}
						return s.flatButton(gtx, &v.bPrint, "Print") // export to HTML
					}),
					layout.Rigid(func(gtx C) D {
						if !v.isDoc() {
							return D{}
						}
						label := "Structure"
						if v.structEdit {
							label = "Done"
						}
						return s.flatButton(gtx, &v.bStruct, label) // node-tree editor
					}),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(6), 0)} }),
					layout.Flexed(1, func(gtx C) D {
						return s.label(gtx, serifFont, font.Bold, font.Regular, 13, v.headerTitle(), cedarBlack, 1)
					}),
				)
			})
		}),
	)

	// When not hovered, cover the menu with a black title bar showing the path.
	if !v.headerHovered {
		fillAt(gtx, cedarBlack, image.Rectangle{Max: dims.Size})
		cgtx := gtx
		cgtx.Constraints.Min = dims.Size
		cgtx.Constraints.Max = dims.Size
		layout.W.Layout(cgtx, func(gtx C) D {
			return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D {
				return s.label(gtx, serifFont, font.Bold, font.Regular, 13, v.headerTitle(), cedarWhite, 1)
			})
		})
	}

	// Header-wide hover zone on top (pass-through) so hovering reveals the menu
	// without stealing button clicks.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	hst := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.headerHovered)
	hst.Pop()
	pass.Pop()

	return dims
}

func (s *gioUI) body(gtx C, v *viewer) D {
	switch v.kind {
	case vkEditor:
		return s.editorBody(gtx, v)
	case vkTerm:
		return s.termBody(gtx, v)
	case vkOutput:
		return s.outputBody(gtx, v)
	case vkCommander:
		return s.commanderBody(gtx, v)
	}
	// The left margin comes from the scroll gutter; add top/right/bottom only.
	if v.isCode {
		if v.editing { // an editable monospace buffer (no highlighting while typing)
			return layout.Inset{Top: 4, Left: 6, Right: 6, Bottom: 4}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				ed := material.Editor(s.th, &v.editor, "")
				ed.Color = cedarBlack
				ed.SelectionColor = cedarGreyMid
				ed.Font = monoFont
				ed.TextSize = s.sp(codeTextSize)
				return ed.Layout(gtx)
			})
		}
		return layout.Inset{Top: 4, Right: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return s.scrollList(gtx, &v.sc, len(v.lines), func(gtx C, i int) D {
				return s.codeLine(gtx, v.lines[i])
			})
		})
	}
	// Documents: the structure editor, or an optional Levels bar over the
	// (optionally depth-capped) blocks.
	if v.structEdit {
		return s.structBody(gtx, v)
	}
	blocks := v.visibleBlocks()
	match := v.currentMatchBlock()
	var markers []string
	if v.showLevels {
		markers = outlineMarkers(blocks)
	}
	if len(v.blockClicks) < len(blocks) {
		v.blockClicks = make([]widget.Clickable, len(blocks))
	}
	docList := func(gtx C) D {
		return layout.Inset{Top: 4, Right: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return s.scrollList(gtx, &v.sc, len(blocks), func(gtx C, i int) D {
				// An artwork node renders its Gargoyle figure in place of the
				// "[Artwork node …]" caption; fall back to the caption if the scene
				// is empty or unparseable.
				if sc := v.scene(blocks[i].Art); !s.artworkOff && sc != nil && !sc.Empty() {
					ok := false
					dims := layout.Center.Layout(gtx, func(gtx C) D {
						d, o := s.artworkBlock(gtx, sc)
						ok = o
						return d
					})
					if ok {
						return dims
					}
				}
				// Click selects a node (shown underlined by docBlock).
				if i < len(v.blockClicks) && v.blockClicks[i].Clicked(gtx) {
					v.selBlock = i
					s.setMessage("selected " + selDesc(blocks[i]))
				}
				block := func(gtx C) D {
					sel := i == v.selBlock
					inner := func(gtx C) D { return s.docBlock(gtx, blocks[i], sel, v) }
					if i == match { // tint the current search-match block
						inner = func(gtx C) D {
							return layout.Stack{}.Layout(gtx,
								layout.Expanded(func(gtx C) D { return fill(gtx, findBg, gtx.Constraints.Min) }),
								layout.Stacked(func(gtx C) D { return s.docBlock(gtx, blocks[i], sel, v) }),
							)
						}
					}
					if i < len(v.blockClicks) {
						return v.blockClicks[i].Layout(gtx, inner)
					}
					return inner(gtx)
				}
				if markers != nil && markers[i] != "" {
					return s.outlineRow(gtx, markers[i], block)
				}
				return block(gtx)
			})
		})
	}
	bars := make([]layout.FlexChild, 0, 4)
	if v.showLevels {
		bars = append(bars,
			layout.Rigid(func(gtx C) D { return s.levelsBar(gtx, v) }),
			layout.Rigid(hrule))
	}
	if v.findOpen {
		bars = append(bars,
			layout.Rigid(func(gtx C) D { return s.findBar(gtx, v) }),
			layout.Rigid(hrule))
	}
	if v.showRefs {
		bars = append(bars,
			layout.Rigid(func(gtx C) D { return s.refsBar(gtx, v) }),
			layout.Rigid(hrule))
	}
	if len(bars) == 0 {
		return docList(gtx)
	}
	bars = append(bars, layout.Flexed(1, docList))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, bars...)
}

// visibleBlocks is the document's blocks filtered to the current level cap
// (levelCap 0 shows everything).
func (v *viewer) visibleBlocks() []tioga.Block {
	if v.levelCap <= 0 {
		return v.blocks
	}
	out := make([]tioga.Block, 0, len(v.blocks))
	for _, b := range v.blocks {
		if b.Depth <= v.levelCap {
			out = append(out, b)
		}
	}
	return out
}

// levelsBar is the outline control revealed by the Levels button: it collapses
// or expands the document by nesting depth (Tioga's FirstLevelOnly / FewerLevels
// / MoreLevels / AllLevels).
func (s *gioUI) levelsBar(gtx C, v *viewer) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	label := "All levels"
	if v.levelCap > 0 {
		label = fmt.Sprintf("Level ≤ %d", v.levelCap)
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			return layout.UniformInset(3).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bLvlFirst, "FirstLevelOnly") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bLvlFewer, "FewerLevels") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bLvlMore, "MoreLevels") }),
					layout.Rigid(func(gtx C) D { return s.flatButton(gtx, &v.bLvlAll, "AllLevels") }),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(8), 0)} }),
					layout.Rigid(func(gtx C) D {
						return s.label(gtx, serifFont, font.Normal, font.Regular, 12, label, cedarBlack, 1)
					}),
				)
			})
		}),
	)
}

// editorBody renders an editable buffer: a serif document editor, or a
// monospace code editor for source files.
func (s *gioUI) editorBody(gtx C, v *viewer) D {
	gtx.Constraints.Min = gtx.Constraints.Max
	fnt, size := serifFont, float32(docTextSize)
	if v.codeEdit {
		fnt, size = monoFont, float32(codeTextSize)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return s.editorToolbar(gtx, v) }),
		layout.Rigid(hrule),
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return layout.Inset{Top: 6, Left: 10, Right: 10, Bottom: 6}.Layout(gtx, func(gtx C) D {
				ed := material.Editor(s.th, &v.editor, "Type here…")
				ed.Color = cedarBlack
				ed.HintColor = cedarGreyMid
				ed.SelectionColor = cedarGreyMid
				ed.Font = fnt
				ed.TextSize = s.sp(size)
				return ed.Layout(gtx)
			})
		}),
	)
}

// editorToolbar is the grey bar above the editor: a filename field, a Save
// button and the last save status.
func (s *gioUI) editorToolbar(gtx C, v *viewer) D {
	v.nameEd.SingleLine = true
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx C) D { return fill(gtx, cedarGrey, gtx.Constraints.Min) }),
		layout.Stacked(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.UniformInset(4).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D {
							return s.label(gtx, serifFont, font.Bold, font.Regular, 13, "File:", cedarBlack, 1)
						})
					}),
					layout.Flexed(1, func(gtx C) D {
						ned := material.Editor(s.th, &v.nameEd, "filename.tioga")
						ned.Color = cedarBlack
						ned.HintColor = cedarGreyMid
						ned.SelectionColor = cedarGreyMid
						ned.Font = monoFont
						ned.TextSize = s.sp(13)
						return ned.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						if !v.runnable {
							return D{}
						}
						return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
							return s.flatButton(gtx, &v.bRun, "Run")
						})
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 6, Right: 8}.Layout(gtx, func(gtx C) D {
							return s.flatButton(gtx, &v.bSave, "Save")
						})
					}),
					layout.Rigid(func(gtx C) D {
						if v.saveMsg == "" {
							return D{}
						}
						return s.label(gtx, serifFont, font.Normal, font.Regular, 12, v.saveMsg, cedarBlack, 1)
					}),
				)
			})
		}),
	)
}

// outputBody renders read-only program output as monospace lines.
func (s *gioUI) outputBody(gtx C, v *viewer) D {
	gtx.Constraints.Min = gtx.Constraints.Max
	lines := strings.Split(v.outText, "\n")
	return layout.Inset{Top: 4, Right: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = gtx.Constraints.Max
		return s.scrollList(gtx, &v.sc, len(lines), func(gtx C, i int) D {
			return s.label(gtx, monoFont, font.Normal, font.Regular, termTextSize, lines[i], cedarBlack, 1)
		})
	})
}

// termBody renders the shell terminal: output fills the whole body and you type
// straight into it (there is no separate command line). Keystrokes are sent raw
// to the pty, which echoes them, so input appears inline with the output.
func (s *gioUI) termBody(gtx C, v *viewer) D {
	t := v.term
	gtx.Constraints.Min = gtx.Constraints.Max

	lines, crow, ccol := t.snapshot()
	// Follow the newest output, but only bottom-anchor once it overflows the
	// viewport; while the output still fits, keep it top-aligned like a real
	// terminal (OffsetLast is the leftover space from the previous layout).
	v.sc.list.List.ScrollToEnd = v.sc.list.List.Position.OffsetLast <= 0

	// A Cedar-style caret marks the insertion point at the cursor: blinking
	// while focused, steady otherwise.
	caretOn := true
	if gtx.Focused(&v.termFocus) {
		caretOn = (gtx.Now.UnixNano()/int64(blinkHalf))%2 == 0
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(blinkHalf)})
	}
	caretRow := -1
	if caretOn {
		caretRow = crow
	}

	dims := layout.Inset{Top: 4, Right: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = gtx.Constraints.Max
		return s.scrollList(gtx, &v.sc, len(lines), func(gtx C, i int) D {
			caretCol := -1
			if i == caretRow {
				caretCol = ccol
			}
			return s.termLine(gtx, lines[i], caretCol)
		})
	})

	// Full-body input zone on top (pass-through, so the scrollbar/wheel still
	// work): click to focus, then keys go to the shell.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.termFocus)
	key.InputHintOp{Tag: &v.termFocus, Hint: key.HintAny}.Add(gtx.Ops)
	area.Pop()
	pass.Pop()
	s.processTermKeys(gtx, v)

	return dims
}

// processTermKeys forwards keystrokes to the shell. Printable text arrives as
// EditEvents; control keys and Ctrl-combos as key.Events mapped to bytes.
func (s *gioUI) processTermKeys(gtx C, v *viewer) {
	t := v.term
	tag := &v.termFocus
	ctrlNames := []key.Name{
		key.NameReturn, key.NameEnter, key.NameDeleteBackward, key.NameDeleteForward,
		key.NameTab, key.NameEscape, key.NameLeftArrow, key.NameRightArrow,
		key.NameUpArrow, key.NameDownArrow, key.NameHome, key.NameEnd,
		key.NamePageUp, key.NamePageDown,
	}
	filters := []event.Filter{
		key.FocusFilter{Target: tag},
		pointer.Filter{Target: tag, Kinds: pointer.Press},
		// Ctrl-combos (Ctrl-C, Ctrl-D, Ctrl-L, ...) produce no text of their own.
		key.Filter{Focus: tag, Name: "", Required: key.ModCtrl, Optional: key.ModShift | key.ModAlt},
	}
	for _, nm := range ctrlNames {
		filters = append(filters, key.Filter{Focus: tag, Name: nm, Optional: key.ModShift | key.ModCtrl | key.ModAlt})
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case pointer.Event:
			if e.Kind == pointer.Press {
				gtx.Execute(key.FocusCmd{Tag: tag})
			}
		case key.EditEvent:
			t.write(e.Text)
		case key.Event:
			if e.State == key.Press {
				t.write(termBytes(e))
			}
		}
	}
}

// termBytes maps a control key event to the bytes a terminal would send.
func termBytes(e key.Event) string {
	if e.Modifiers.Contain(key.ModCtrl) && len(e.Name) == 1 {
		c := e.Name[0]
		switch {
		case c >= 'A' && c <= 'Z':
			return string([]byte{c - 'A' + 1})
		case c >= 'a' && c <= 'z':
			return string([]byte{c - 'a' + 1})
		}
	}
	switch e.Name {
	case key.NameReturn, key.NameEnter:
		return "\r"
	case key.NameDeleteBackward:
		return "\x7f"
	case key.NameDeleteForward:
		return "\x1b[3~"
	case key.NameTab:
		return "\t"
	case key.NameEscape:
		return "\x1b"
	case key.NameLeftArrow:
		return "\x1b[D"
	case key.NameRightArrow:
		return "\x1b[C"
	case key.NameUpArrow:
		return "\x1b[A"
	case key.NameDownArrow:
		return "\x1b[B"
	case key.NameHome:
		return "\x1b[H"
	case key.NameEnd:
		return "\x1b[F"
	case key.NamePageUp:
		return "\x1b[5~"
	case key.NamePageDown:
		return "\x1b[6~"
	}
	return ""
}

// termLabel draws a run of terminal text.
func (s *gioUI) termLabel(gtx C, text string) D {
	return s.label(gtx, monoFont, font.Normal, font.Regular, termTextSize, text, cedarBlack, 1)
}

// termLine draws one terminal output line. caretCol >= 0 places the insertion
// caret at that rune column (splitting the line around it).
func (s *gioUI) termLine(gtx C, text string, caretCol int) D {
	if caretCol < 0 {
		return s.termLabel(gtx, text)
	}
	r := []rune(text)
	prefix := text
	suffix := ""
	if caretCol < len(r) {
		prefix, suffix = string(r[:caretCol]), string(r[caretCol:])
	} else if caretCol > len(r) {
		prefix = text + strings.Repeat(" ", caretCol-len(r)) // cursor past line end
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return s.termLabel(gtx, prefix) }),
		layout.Rigid(func(gtx C) D { return s.termCaret(gtx) }),
		layout.Rigid(func(gtx C) D { return s.termLabel(gtx, suffix) }),
	)
}

// termCaret draws the Cedar insertion caret: a small, bold up-pointing wedge
// sitting on the text baseline.
func (s *gioUI) termCaret(gtx C) D {
	em := float32(gtx.Sp(s.sp(termTextSize)))
	w := int(em * 0.6)
	h := int(em * 0.44)
	pad := int(em * 0.26) // drop to roughly the baseline
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(0, float32(h)))
	p.LineTo(f32.Pt(float32(w)/2, 0))
	p.LineTo(f32.Pt(float32(w), float32(h)))
	stroke := clip.Stroke{Path: p.End(), Width: em * 0.18}.Op() // bold
	off := op.Offset(image.Pt(0, pad)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, cedarBlack, stroke)
	off.Pop()
	return D{Size: image.Pt(w, h+pad)}
}

func (s *gioUI) codeLine(gtx C, runs []run) D {
	children := make([]layout.FlexChild, len(runs))
	for i := range runs {
		r := runs[i]
		bold, ital := styleFor(r.cat)
		weight, style := font.Normal, font.Regular
		if bold {
			weight = font.Bold
		}
		if ital {
			style = font.Italic
		}
		children[i] = layout.Rigid(func(gtx C) D {
			return s.label(gtx, monoFont, weight, style, codeTextSize, r.text, cedarBlack, 1)
		})
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
}

// docTextSize / docLineHeight tune document readability (roomier than the old
// cramped defaults); both scale further with zoom.
const (
	docTextSize   = 18
	docLineHeight = 1.3
)

func (s *gioUI) docBlock(gtx C, b tioga.Block, selected bool, v *viewer) D {
	// A tab-aligned, multi-row block is a table: lay it out as an aligned grid
	// rather than approximating the document's tab ruler.
	if looksLikeTable(b) {
		return s.tableBlock(gtx, b)
	}
	// The node's named Tioga format (resolved through the default Cedar style)
	// sets the face, size, alignment, spacing and base indent; a comment node
	// falls back to italic like the old Quote rendering.
	st := docStyle(b.Format, b.Depth)
	if b.Kind == tioga.Quote && b.Format == "" {
		st.fnt.Style = font.Italic
	}
	col := blockColor(b)
	return layout.Inset{Top: st.above, Bottom: st.below, Left: st.indent}.Layout(gtx, func(gtx C) D {
		// A selected node is shown fully underlined (Tioga's selection feedback), so
		// route it through the rich-text path with underline forced on every run.
		if selected {
			runs := runsForBlock(b)
			for i := range runs {
				runs[i].Look |= uLook
			}
			return s.richText(gtx, runs, st.fnt, st.size, col, docLineHeight, st.align, v)
		}
		// When the block carries real looks, render its runs so the file's own
		// bold/italic/underline show through; the format still sets the base face.
		if hasLooks(b.Runs) {
			return s.richText(gtx, b.Runs, st.fnt, st.size, col, docLineHeight, st.align, v)
		}
		return s.paraLabel(gtx, st.fnt, st.size, st.align, docLineHeight, b.Text, col)
	})
}

// selDesc describes a selected block for the message line.
func selDesc(b tioga.Block) string {
	f := b.Format
	if f == "" {
		f = "node"
	}
	t := strings.TrimSpace(b.Text)
	if len(t) > 40 {
		t = t[:40] + "…"
	}
	if t == "" {
		return f
	}
	return f + ": " + t
}

// uLook is the underline look bit.
const uLook = tioga.Look(1 << (31 - ('u' - 'a')))

// runsForBlock returns a block's runs, or a single plain run from its text.
func runsForBlock(b tioga.Block) []tioga.Run {
	if len(b.Runs) > 0 {
		out := make([]tioga.Run, len(b.Runs))
		copy(out, b.Runs)
		return out
	}
	return []tioga.Run{{Text: b.Text}}
}

// blockColor is a block's text colour: its CharProps text colour if set (a
// postfix property), else the default black.
func blockColor(b tioga.Block) color.NRGBA {
	if b.Fg == nil {
		return cedarBlack
	}
	cl := func(f float32) uint8 {
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		return uint8(f*255 + 0.5)
	}
	return color.NRGBA{R: cl(b.Fg.R), G: cl(b.Fg.G), B: cl(b.Fg.B), A: 0xff}
}

// paraLabel draws a wrapped, aligned plain-text paragraph (the no-looks path).
func (s *gioUI) paraLabel(gtx C, fnt font.Font, size float32, align text.Alignment, lineHeight float32, txt string, col color.NRGBA) D {
	macro := op.Record(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	cl := macro.Stop()
	l := widget.Label{MaxLines: 0, LineHeightScale: lineHeight, Alignment: align}
	return l.Layout(gtx, s.sh, fnt, s.sp(size), txt, cl)
}

// expandTabs replaces tab characters with spaces to the next 8-column stop
// (per line). Gio's text renderer draws a raw tab as a missing-glyph box, so
// Tioga's tab-aligned blocks (e.g. address lists) would otherwise show boxes.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	const tw = 8
	var b strings.Builder
	b.Grow(len(s))
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			n := tw - col%tw
			for i := 0; i < n; i++ {
				b.WriteByte(' ')
			}
			col += n
		case '\n':
			b.WriteByte('\n')
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// styleFor returns emphasis for a category: black text, distinguished by
// bold/italic only (Tioga's monochrome "looks").
func styleFor(cat cedar.Category) (bold, italic bool) {
	st := cedar.CategoryStyleMono(cat)
	return st.Bold, st.Italic
}

// codeToRuns turns highlighted code into per-line styled runs (computed once).
func codeToRuns(code string, builtins map[string]bool) [][]run {
	lines := strings.Split(code, "\n")
	byRow := map[int][]cedar.Span{}
	for _, sp := range cedar.Highlight(code, builtins) {
		byRow[sp.Row] = append(byRow[sp.Row], sp)
	}
	out := make([][]run, len(lines))
	for r, line := range lines {
		runes := []rune(line)
		spans := byRow[r]
		sort.Slice(spans, func(i, j int) bool { return spans[i].Col < spans[j].Col })
		var rs []run
		pos := 0
		for _, sp := range spans {
			if sp.Col > len(runes) {
				continue
			}
			if sp.Col > pos {
				rs = append(rs, run{string(runes[pos:sp.Col]), cedar.C_Ident})
			}
			end := sp.Col + sp.Len
			if end > len(runes) {
				end = len(runes)
			}
			if end > sp.Col {
				rs = append(rs, run{string(runes[sp.Col:end]), sp.Cat})
				pos = end
			}
		}
		if pos < len(runes) {
			rs = append(rs, run{string(runes[pos:]), cedar.C_Ident})
		}
		if len(rs) == 0 {
			rs = append(rs, run{" ", cedar.C_Ident})
		}
		out[r] = rs
	}
	return out
}
