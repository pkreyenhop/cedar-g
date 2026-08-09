package main

import "testing"

func TestScanCwd(t *testing.T) {
	term := &terminal{home: "/Users/bob"}
	// A prompt-emitted OSC 7 report with a percent-encoded path.
	term.feed([]byte("some output\x1b]7;file://Petes-Mac/Users/bob/src/cedar%20g\x07more"))
	if got, want := term.dir(), "~/src/cedar g"; got != want {
		t.Fatalf("dir() = %q, want %q", got, want)
	}
	// A path outside home is shown verbatim.
	term.feed([]byte("\x1b]7;file://host/tmp/x\x07"))
	if got, want := term.dir(), "/tmp/x"; got != want {
		t.Fatalf("dir() = %q, want %q", got, want)
	}
}

// TestFeedEditing checks the emulator handles the redraw/backspace sequences a
// shell emits, so the first letter isn't duplicated and backspace erases.
func TestFeedEditing(t *testing.T) {
	// Carriage-return redraw must overwrite the line, not duplicate it.
	term := &terminal{}
	term.feed([]byte("% \rl\r% ls"))
	if got := lastLine(term); got != "% ls" {
		t.Fatalf("cr redraw: last line = %q, want %q", got, "% ls")
	}
	// Backspace + erase-to-end-of-line must remove the character.
	term2 := &terminal{}
	term2.feed([]byte("% ls"))
	term2.feed([]byte("\b\x1b[K")) // back over 's', erase to EOL
	if got := lastLine(term2); got != "% l" {
		t.Fatalf("backspace: last line = %q, want %q", got, "% l")
	}
}

func lastLine(t *terminal) string {
	lines, _, _ := t.snapshot()
	return lines[len(lines)-1]
}
