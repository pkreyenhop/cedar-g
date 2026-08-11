package main

import (
	"testing"

	"cedarg/internal/tioga"
)

func TestOutlineMarkers(t *testing.T) {
	blocks := []tioga.Block{
		{Kind: tioga.Heading, Level: 1}, // I.
		{Kind: tioga.Paragraph},         // (none)
		{Kind: tioga.Heading, Level: 2}, // A.
		{Kind: tioga.Heading, Level: 2}, // B.
		{Kind: tioga.Heading, Level: 1}, // II.
		{Kind: tioga.Heading, Level: 2}, // A. (reset under II)
		{Kind: tioga.Heading, Level: 3}, // 1.
	}
	got := outlineMarkers(blocks)
	want := []string{"I.", "", "A.", "B.", "II.", "A.", "1."}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("marker[%d] = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestRomanAndAlpha(t *testing.T) {
	for n, w := range map[int]string{1: "I", 4: "IV", 9: "IX", 14: "XIV", 40: "XL"} {
		if toRoman(n) != w {
			t.Fatalf("roman %d = %q, want %q", n, toRoman(n), w)
		}
	}
	for n, w := range map[int]string{1: "A", 26: "Z", 27: "AA", 28: "AB"} {
		if toAlpha(n) != w {
			t.Fatalf("alpha %d = %q, want %q", n, toAlpha(n), w)
		}
	}
}
