package main

import (
	"os"
	"path/filepath"
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
