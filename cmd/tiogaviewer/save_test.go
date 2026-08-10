package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cedarg/internal/tioga"
)

// TestSaveEditorRoundTrips saves an editor buffer and reopens it as a viewer.
func TestSaveEditorRoundTrips(t *testing.T) {
	dir := t.TempDir()
	s := newUI()
	s.setRoot(dir)

	v := s.newEditorViewer()
	v.editor.SetText("First paragraph\nSecond paragraph")
	v.nameEd.SetText("mydoc") // no extension: Save should add .tioga

	s.saveEditor(v)

	path := filepath.Join(dir, "mydoc.tioga")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if v.saveMsg == "" || v.path != path {
		t.Fatalf("save state not updated: msg=%q path=%q", v.saveMsg, v.path)
	}

	// Reopen through the normal file path and confirm the paragraphs survive.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := tioga.Read(data, false)
	if len(doc.Blocks) != 2 ||
		doc.Blocks[0].Text != "First paragraph" ||
		doc.Blocks[1].Text != "Second paragraph" {
		t.Fatalf("round-trip mismatch: %+v", doc.Blocks)
	}
}

// TestEditorSaveReopensHighlighted checks the round trip: type Mesa code into a
// New Document, save it as a Tioga file, reopen it, and confirm the viewer
// treats it as syntax-highlighted code (not a prose document).
func TestEditorSaveReopensHighlighted(t *testing.T) {
	dir := t.TempDir()
	s := newUI()
	s.setRoot(dir)

	v := s.newEditorViewer()
	code := "HelloWorld: CEDAR PROGRAM ~ {\n" +
		"  x: INT ← 5;\n" +
		"  IO.PutLine[\"hi\"];\n" +
		"}.\n"
	v.editor.SetText(code)
	v.nameEd.SetText("Hello") // saves Hello.tioga
	s.saveEditor(v)

	path := filepath.Join(dir, "Hello.tioga")
	nv := s.newViewer(path)
	if !nv.isCode {
		t.Fatalf("reopened editor code should be highlighted (isCode), got a document")
	}
	if len(nv.lines) == 0 {
		t.Fatalf("no highlighted lines produced")
	}
	if !strings.Contains(nv.src, "PROGRAM") {
		t.Fatalf("source not preserved: %q", nv.src)
	}
}

// TestProseTiogaStaysDocument guards that a non-code Tioga document is not
// mistaken for code.
func TestProseTiogaStaysDocument(t *testing.T) {
	dir := t.TempDir()
	s := newUI()
	s.setRoot(dir)
	path := filepath.Join(dir, "Paper.tioga")
	prose := "Using Threads in Interactive Systems\n\nWe describe the results of examining two large systems.\n"
	if err := os.WriteFile(path, tioga.Encode(prose), 0o644); err != nil {
		t.Fatal(err)
	}
	v := s.newViewer(path)
	if v.isCode {
		t.Fatalf("prose document should not be treated as code")
	}
}
