package main

import (
	"io"
	"os"
	"strings"

	"gioui.org/io/clipboard"

	"cedarg/internal/tioga"
)

// copyNode copies the selected node's text to the system clipboard — Cedar's
// Copy, on the current selection.
func (s *gioUI) copyNode(gtx C, v *viewer) {
	blocks := v.visibleBlocks()
	if v.selBlock < 0 || v.selBlock >= len(blocks) {
		s.setMessage("nothing selected")
		return
	}
	txt := strings.TrimSpace(blocks[v.selBlock].Text)
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(txt))})
	s.setMessage("copied node")
}

// reloadViewer reverts a document/code viewer to its file on disk — Cedar's
// Reset gesture — discarding any edits and leaving structure editing.
func (s *gioUI) reloadViewer(v *viewer) {
	if v.path == "" {
		s.setMessage("nothing to reset")
		return
	}
	data, err := os.ReadFile(v.path)
	if err != nil {
		s.setMessage("reset failed: " + err.Error())
		return
	}
	if v.isCode {
		code := tioga.Read(data, true).Code
		v.src = code
		v.lines = codeToRuns(expandTabs(code), s.builtins)
		v.editing, v.editorInit = false, false
	} else {
		doc := tioga.Read(data, false)
		doc.Blocks = mergeTableBlocks(doc.Blocks)
		for i := range doc.Blocks {
			if !looksLikeTable(doc.Blocks[i]) {
				doc.Blocks[i].Text = expandTabs(doc.Blocks[i].Text)
			}
		}
		v.blocks = doc.Blocks
		v.root = doc.Root
	}
	v.selBlock = -1
	v.blockClicks = nil
	v.structEdit, v.doc = false, nil
	v.undoStack, v.redoStack = nil, nil
	s.setMessage("reset " + v.rel)
}
