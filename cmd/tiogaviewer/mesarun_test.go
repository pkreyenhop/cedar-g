package main

import (
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

	s.runEditor(v)
	if v.runOut == nil || !strings.Contains(v.runOut.outText, "Hello from Mesa!") {
		t.Fatalf("first run: %+v", v.runOut)
	}
	before := len(s.allViewers())
	first := v.runOut

	s.runEditor(v) // second run should reuse the same output viewer
	if v.runOut != first {
		t.Fatalf("output viewer not reused")
	}
	if got := len(s.allViewers()); got != before {
		t.Fatalf("viewer count changed on rerun: %d -> %d", before, got)
	}
}
