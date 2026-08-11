package main

import (
	"testing"

	"cedarg/internal/tioga"
)

func TestFindMatches(t *testing.T) {
	v := &viewer{findOpen: true}
	blocks := []tioga.Block{
		{Text: "The quick brown fox"},
		{Text: "jumps over"},
		{Text: "the lazy Dog"},
		{Text: "THE END"},
	}
	v.findQuery = "the"
	v.recomputeMatches(blocks)
	// case-insensitive: blocks 0, 2, 3 contain "the"
	if len(v.findMatches) != 3 || v.findMatches[0] != 0 || v.findMatches[2] != 3 {
		t.Fatalf("matches = %v", v.findMatches)
	}
	if v.currentMatchBlock() != 0 {
		t.Fatalf("first match block = %d", v.currentMatchBlock())
	}
	v.findIdx = 2
	if v.currentMatchBlock() != 3 {
		t.Fatalf("third match block = %d", v.currentMatchBlock())
	}

	// No query clears matches.
	v.findQuery = ""
	v.recomputeMatches(blocks)
	if len(v.findMatches) != 0 || v.currentMatchBlock() != -1 {
		t.Fatalf("empty query should have no matches")
	}

	// Closed find reports no current match even with stale matches.
	v.findOpen = false
	v.findQuery = "the"
	v.recomputeMatches(blocks)
	if v.currentMatchBlock() != -1 {
		t.Fatalf("closed find should not report a match")
	}
}
