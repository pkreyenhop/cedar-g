package main

import "testing"

func TestScanCwd(t *testing.T) {
	term := &terminal{home: "/Users/bob"}
	// A prompt-emitted OSC 7 report with a percent-encoded path.
	raw := []byte("some output\x1b]7;file://Petes-Mac/Users/bob/src/cedar%20g\x07more")
	term.scanCwd(raw)
	if got, want := term.dir(), "~/src/cedar g"; got != want {
		t.Fatalf("dir() = %q, want %q", got, want)
	}
	// A path outside home is shown verbatim.
	term.scanCwd([]byte("\x1b]7;file://host/tmp/x\x07"))
	if got, want := term.dir(), "/tmp/x"; got != want {
		t.Fatalf("dir() = %q, want %q", got, want)
	}
}
