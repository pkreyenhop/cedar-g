package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenTextFileIsEditable(t *testing.T) {
	dir := t.TempDir()
	src := "package main\n\nfunc main() {}\n"
	path := filepath.Join(dir, "hello.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newUI()
	s.setRoot(dir)
	v := s.newViewer(path)

	if v.kind != vkEditor {
		t.Fatalf("expected an editable viewer, got kind %d", v.kind)
	}
	if !v.plainText || !v.codeEdit {
		t.Fatalf("go file should be plainText code edit: %+v", v)
	}
	if v.runnable {
		t.Fatalf("a .go file must not be runnable as Mesa")
	}
	if v.editor.Text() != src {
		t.Fatalf("editor content = %q, want %q", v.editor.Text(), src)
	}
}

func TestSavePlainTextRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newUI()
	s.setRoot(dir)
	v := s.newViewer(path)
	v.editor.SetText("# Title\n\nEdited body.\n")
	s.saveEditor(v)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Title\n\nEdited body.\n" {
		t.Fatalf("saved bytes = %q", got)
	}
	// A markdown file uses the serif face, not code.
	if v.codeEdit {
		t.Fatalf(".md should not be a code edit")
	}
}

func TestTreeShowsNewFormats(t *testing.T) {
	for _, name := range []string{"a.txt", "b.md", "c.go", "d.c"} {
		if !matchFile(name) {
			t.Fatalf("tree should list %s", name)
		}
	}
}
