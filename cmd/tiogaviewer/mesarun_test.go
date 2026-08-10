package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const helloMesa = `HelloWorld: PROGRAM =
BEGIN
  IO.PutLine["Hello from Mesa!"];
END.`

func TestRunMesaOutput(t *testing.T) {
	out := runMesa(helloMesa)
	if !strings.Contains(out, "Hello from Mesa!") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunMesaParseError(t *testing.T) {
	out := runMesa("this is not mesa")
	if !strings.Contains(out, "error") {
		t.Fatalf("expected an error, got: %q", out)
	}
}

func TestRunEditorReusesOutputViewer(t *testing.T) {
	s := newUI()
	v := s.newEditorViewer()
	v.editor.SetText(helloMesa)

	s.runViewer(v)
	if v.runOut == nil || !strings.Contains(v.runOut.outText, "Hello from Mesa!") {
		t.Fatalf("first run: %+v", v.runOut)
	}
	before := len(s.allViewers())
	first := v.runOut

	s.runViewer(v) // second run should reuse the same output viewer
	if v.runOut != first {
		t.Fatalf("output viewer not reused")
	}
	if got := len(s.allViewers()); got != before {
		t.Fatalf("viewer count changed on rerun: %d -> %d", before, got)
	}
}

func TestOpenMesaFileIsRunnable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Hi.mesa")
	if err := os.WriteFile(path, []byte(helloMesa), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newUI()
	s.setRoot(dir)
	v := s.newViewer(path)
	if v.kind != vkContent || !v.runnable || v.src == "" {
		t.Fatalf(".mesa viewer should be read-only content but runnable: %+v kind/runnable/src", v.kind)
	}
	s.runViewer(v)
	if v.runOut == nil || !strings.Contains(v.runOut.outText, "Hello from Mesa!") {
		t.Fatalf("running the opened .mesa did not produce output: %+v", v.runOut)
	}
}

// TestEditMesaAndRun opens a .mesa file, switches to edit mode, modifies the
// buffer, and runs it — the output must reflect the edit.
func TestEditMesaAndRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Hi.mesa")
	orig := "Hi: PROGRAM =\nBEGIN\n  IO.PutLine[\"one\"];\nEND."
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newUI()
	s.setRoot(dir)
	v := s.newViewer(path)
	if !v.isCode {
		t.Fatalf("expected a code viewer")
	}
	s.toggleEdit(v) // enter edit mode
	if !v.editing || !v.editorInit {
		t.Fatalf("edit mode not entered")
	}
	// modify the buffer
	v.editor.SetText("Hi: PROGRAM =\nBEGIN\n  IO.PutLine[\"two\"];\nEND.")
	s.runViewer(v)
	if v.runOut == nil || !strings.Contains(v.runOut.outText, "two") {
		t.Fatalf("run did not reflect the edit: %+v", v.runOut)
	}
	// back to view mode: re-highlighted from the edited source
	s.toggleEdit(v)
	if v.editing || !strings.Contains(v.src, "two") || len(v.lines) == 0 {
		t.Fatalf("view mode did not pick up edits: editing=%v src=%q", v.editing, v.src)
	}
}
