package main

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"

	"cedarg/internal/tioga"
)

// TestUISmoke builds the UI headlessly and drives the tile pipeline to ensure
// the Fyne wiring does not panic. It does not require a display.
func TestUISmoke(t *testing.T) {
	test.NewApp()
	w := test.NewWindow(nil)
	u := newUI(w)

	// A code file and a document file.
	dir := t.TempDir()
	codeF := filepath.Join(dir, "Bar.mesa")
	docF := filepath.Join(dir, "Doc.tioga")
	if err := os.WriteFile(codeF, []byte("Bar: CEDAR PROC = { n: INTEGER \xac 42 };"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docF, []byte("Just some document text."), 0o644); err != nil {
		t.Fatal(err)
	}

	u.setRoot(dir)
	if kids := u.childUIDs(""); len(kids) != 2 {
		t.Fatalf("tree children = %v, want 2", kids)
	}

	// Opening two files creates two viewers, balanced across the two columns.
	u.openFile(codeF)
	u.openFile(docF)
	if n := len(u.allViewers()); n != 2 {
		t.Fatalf("viewers = %d, want 2", n)
	}
	if len(u.columns[0]) != 1 || len(u.columns[1]) != 1 {
		t.Errorf("columns = %d,%d, want 1,1", len(u.columns[0]), len(u.columns[1]))
	}
	// Reopening the same file must not create another viewer.
	u.openFile(codeF)
	if n := len(u.allViewers()); n != 2 {
		t.Fatalf("viewers after reopen = %d, want 2", n)
	}

	// Monochrome toggle restyles code viewers without panicking.
	u.setMono(true)
	u.setMono(false)

	v := u.columns[0][0]
	// Grow toggles.
	u.growViewer(v)
	if u.grown[v.col] != v {
		t.Errorf("grow not set")
	}
	u.growViewer(v)
	if u.grown[v.col] != nil {
		t.Errorf("grow not cleared")
	}
	// Split adds a sibling below in the same column.
	u.splitViewer(v)
	if len(u.columns[v.col]) != 2 {
		t.Fatalf("after split column = %d, want 2", len(u.columns[v.col]))
	}
	// Switch moves it to the other column.
	from := v.col
	u.switchViewer(v)
	if v.col == from {
		t.Errorf("switch did not change column")
	}
	// Minimize parks it in the tray; restore returns it.
	u.minimizeViewer(v)
	if len(u.minimized) != 1 {
		t.Fatalf("minimized = %d, want 1", len(u.minimized))
	}
	u.restoreViewer(v)
	if len(u.minimized) != 0 {
		t.Fatalf("still minimized = %d", len(u.minimized))
	}
	// Destroy removes just this viewer (the split sibling remains).
	u.destroyViewer(v)
	if n := len(u.allViewers()); n != 2 {
		t.Fatalf("viewers after destroy = %d, want 2", n)
	}
}

func TestZoomClamp(t *testing.T) {
	test.NewApp()
	u := newUI(test.NewWindow(nil))
	if u.fontScale != 1.0 {
		t.Fatalf("default fontScale = %v, want 1.0", u.fontScale)
	}
	// Zoom out far past the floor: must clamp at minFontScale.
	for i := 0; i < 40; i++ {
		u.zoomBy(-0.1)
	}
	if u.fontScale < minFontScale-1e-6 {
		t.Errorf("fontScale under floor: %v", u.fontScale)
	}
	// Zoom in far past the ceiling: must clamp at maxFontScale.
	for i := 0; i < 120; i++ {
		u.zoomBy(+0.1)
	}
	if u.fontScale > maxFontScale+1e-6 {
		t.Errorf("fontScale over ceiling: %v", u.fontScale)
	}
	u.zoomReset()
	if u.fontScale != 1.0 {
		t.Errorf("after reset fontScale = %v, want 1.0", u.fontScale)
	}
}

// TestBlockSegmentsBreakLines guards that heading/quote/paragraph blocks each
// render on their own line (Inline == false), so a Tioga document's line
// structure is preserved instead of blocks running together.
func TestBlockSegmentsBreakLines(t *testing.T) {
	kinds := []tioga.BlockKind{tioga.Heading, tioga.Quote, tioga.Paragraph, tioga.Code}
	for _, k := range kinds {
		seg := blockSegment(tioga.Block{Kind: k, Level: 1, Text: "x"})
		if seg.Inline() {
			t.Errorf("block kind %d rendered inline; want its own line", k)
		}
	}
}
