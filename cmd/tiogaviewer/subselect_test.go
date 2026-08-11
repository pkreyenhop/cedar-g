package main

import (
	"testing"

	"cedarg/internal/tioga"
)

func TestApplyLookRange(t *testing.T) {
	d := tioga.NewDoc(nil)
	n := d.InsertSibling(nil, tioga.NewNode("body", "Hello World"))
	// Bold just "World" (runes 6..11).
	d.ApplyLookRange(n, 6, 11, 'b')
	// Expect two runs: "Hello " plain, "World" bold.
	if len(n.Runs) != 2 {
		t.Fatalf("want 2 runs, got %d: %+v", len(n.Runs), n.Runs)
	}
	if n.Runs[0].Text != "Hello " || n.Runs[0].Look.Bold() {
		t.Fatalf("run0 = %+v", n.Runs[0])
	}
	if n.Runs[1].Text != "World" || !n.Runs[1].Look.Bold() {
		t.Fatalf("run1 = %+v", n.Runs[1])
	}
	// Re-applying over the same range clears it (all had the look).
	d.ApplyLookRange(n, 6, 11, 'b')
	if n.Runs[0].Look.Bold() || (len(n.Runs) > 1 && n.Runs[1].Look.Bold()) {
		t.Fatalf("toggle-off failed: %+v", n.Runs)
	}
	if n.Text() != "Hello World" {
		t.Fatalf("text changed: %q", n.Text())
	}
}

func TestApplyLookRangePartialOverlap(t *testing.T) {
	d := tioga.NewDoc(nil)
	n := d.InsertSibling(nil, tioga.NewNode("body", "abcdef"))
	d.ApplyLookRange(n, 0, 3, 'i') // italic abc
	d.ApplyLookRange(n, 2, 5, 'i') // italic cde — overlaps; "abcde" not all italic yet -> set all
	// c,d,e become italic; a,b already were. Now a..e italic, f plain.
	got := ""
	for _, r := range n.Runs {
		if r.Look.Italic() {
			got += r.Text
		}
	}
	if got != "abcde" {
		t.Fatalf("italic span = %q, want abcde", got)
	}
}
