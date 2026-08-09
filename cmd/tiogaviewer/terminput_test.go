package main

import (
	"image"
	"os"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestTermInput proves that clicking the terminal focuses it and that a typed
// character is forwarded to the pty. A pipe stands in for the pty so the bytes
// written by the input handler can be read back.
func TestTermInput(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()

	s := newUI()
	v := &viewer{kind: vkTerm, term: &terminal{ptmx: pw}}

	var ops op.Ops
	var q input.Router
	size := image.Pt(200, 200)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(size),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Source:      q.Source(),
		}
		s.termBody(gtx, v)
		q.Frame(&ops)
	}

	frame() // establish the input areas
	// Click inside the terminal body to focus it.
	q.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(60, 60)})
	frame()
	frame() // let the focus command take effect
	// Type a character; on macOS this arrives as an EditEvent.
	q.Queue(key.EditEvent{Text: "x"})
	frame()

	done := make(chan string, 1)
	go func() {
		b := make([]byte, 4)
		n, _ := pr.Read(b)
		done <- string(b[:n])
	}()
	select {
	case got := <-done:
		if got != "x" {
			t.Fatalf("pty received %q, want %q", got, "x")
		}
	case <-timeAfter():
		t.Fatal("timed out: no bytes forwarded to the pty (terminal not focused?)")
	}

	// Enter arrives as a key.Event and must be forwarded as a carriage return.
	q.Queue(key.Event{Name: key.NameReturn, State: key.Press})
	frame()
	done2 := make(chan string, 1)
	go func() {
		b := make([]byte, 4)
		n, _ := pr.Read(b)
		done2 <- string(b[:n])
	}()
	select {
	case got := <-done2:
		if got != "\r" {
			t.Fatalf("pty received %q for Enter, want %q", got, "\r")
		}
	case <-timeAfter():
		t.Fatal("timed out: Enter not forwarded to the pty")
	}
}
