package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func lastLog(v *viewer) string { return strings.Join(v.cmdLog, "\n") }

func TestCommander(t *testing.T) {
	s := newUI()
	samples, _ := filepath.Abs("../../internal/mesa/samples")
	s.setRoot(samples)
	v := s.newCommanderViewer()

	// Help lists commands.
	s.runCommand(v, "Help")
	if !strings.Contains(lastLog(v), "Artwork on|off") {
		t.Fatalf("Help missing: %s", lastLog(v))
	}

	// Artwork off toggles the global flag.
	s.runCommand(v, "Artwork off")
	if !s.artworkOff {
		t.Fatal("Artwork off did not set the flag")
	}
	s.runCommand(v, "Artwork on")
	if s.artworkOff {
		t.Fatal("Artwork on did not clear the flag")
	}

	// Run executes a Mesa program and logs its output.
	s.runCommand(v, "Run AoCDay1.mesa")
	log := lastLog(v)
	if !strings.Contains(log, "11") || !strings.Contains(log, "31") {
		t.Fatalf("Run output missing 11/31: %s", log)
	}

	// Unknown command is reported.
	s.runCommand(v, "Frobnicate")
	if !strings.Contains(lastLog(v), "unknown command") {
		t.Fatalf("unknown command not reported")
	}

	// Clear empties the log.
	s.runCommand(v, "Clear")
	if len(v.cmdLog) != 0 {
		t.Fatalf("Clear left %d lines", len(v.cmdLog))
	}
}
