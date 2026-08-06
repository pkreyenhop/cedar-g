package main

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"

	"cedarg/internal/tioga"
)

// TestUISmoke builds the UI headlessly and drives the render paths to ensure the
// Fyne wiring does not panic. It does not require a display.
func TestUISmoke(t *testing.T) {
	test.NewApp()

	w := test.NewWindow(nil)
	u := newUI(w)

	// Document rendering path.
	u.showDoc([]tioga.Block{
		{Kind: tioga.Heading, Level: 1, Text: "Title"},
		{Kind: tioga.Paragraph, Text: "Some prose."},
		{Kind: tioga.Quote, Text: "An aside."},
		{Kind: tioga.Code, Text: "x ← 1"},
	})

	// Code rendering + highlighting path (with the "←" assignment byte).
	u.showCode("Foo: CEDAR DEFINITIONS = BEGIN\n  n: INTEGER \xac 42;\nEND.")

	// Open a real file through the full pipeline.
	dir := t.TempDir()
	f := filepath.Join(dir, "Bar.mesa")
	if err := os.WriteFile(f, []byte("Bar: PROC = { RETURN };"), 0o644); err != nil {
		t.Fatal(err)
	}
	u.setRoot(dir)
	if kids := u.childUIDs(""); len(kids) != 1 || filepath.Base(kids[0]) != "Bar.mesa" {
		t.Fatalf("tree children = %v", kids)
	}
	u.openFile(f)
	if u.title.Text == "" {
		t.Fatalf("title not set after openFile")
	}
}

func TestZoomClamp(t *testing.T) {
	test.NewApp()
	u := newUI(test.NewWindow(nil))
	if u.fontScale != 1.0 {
		t.Fatalf("default fontScale = %v, want 1.0", u.fontScale)
	}
	// Zoom out far past the floor: must clamp at 0.6.
	for i := 0; i < 20; i++ {
		u.zoomBy(-0.1)
	}
	if u.fontScale < 0.6-1e-6 {
		t.Errorf("fontScale under floor: %v", u.fontScale)
	}
	// Zoom in far past the ceiling: must clamp at 3.0.
	for i := 0; i < 60; i++ {
		u.zoomBy(+0.1)
	}
	if u.fontScale > 3.0+1e-6 {
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
