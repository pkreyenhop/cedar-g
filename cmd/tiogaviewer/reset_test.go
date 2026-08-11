package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cedarg/internal/tioga"
)

func TestReloadViewer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Doc.tioga")
	d0 := tioga.NewDoc(nil)
	d0.InsertSibling(nil, tioga.NewNode("head", "Original heading"))
	if err := os.WriteFile(path, tioga.EncodeDoc(d0.Root), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newUI()
	s.setRoot(dir)
	v := s.newViewer(path)

	// Simulate edits, then Reset.
	v.blocks = []tioga.Block{{Text: "unsaved change"}}
	v.selBlock = 3
	v.structEdit = true
	s.reloadViewer(v)

	found := false
	for _, b := range v.blocks {
		if strings.Contains(b.Text, "Original heading") {
			found = true
		}
	}
	if !found {
		t.Fatalf("reset did not restore from file: %+v", v.blocks)
	}
	if v.selBlock != -1 || v.structEdit {
		t.Fatalf("reset should clear selection and leave struct edit")
	}
}
