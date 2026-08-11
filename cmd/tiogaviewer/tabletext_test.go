package main

import (
	"strings"
	"testing"

	"cedarg/internal/tioga"
)

func cellsOf(row []tcell) []string {
	out := make([]string, len(row))
	for i, c := range row {
		out[i] = strings.TrimSpace(c.text())
	}
	return out
}

// TestTableGridVaryingTabs is the core case: rows that reach the same columns
// with different tab counts (and space padding) must split to the same cells.
func TestTableGridVaryingTabs(t *testing.T) {
	bold := tioga.Look(1 << (31 - ('b' - 'a')))
	// "Defer work" uses 2 tabs; "Deadlock avoidance" uses 1 tab; both 5 columns.
	runs := []tioga.Run{
		{Text: "Defer work", Look: bold},
		{Text: "\t\t 108\t  31%\t   77\t 33%\t\n"},
		{Text: "Deadlock avoidance", Look: bold},
		{Text: "\t   35\t  10%\t     6\t  3%\t\n"},
		{Text: "General pumps", Look: bold},
		{Text: "\t   \t  48\t  14%\t   33\t14%\t"},
	}
	grid := tableGrid(runs)
	if len(grid) != 3 {
		t.Fatalf("want 3 rows, got %d: %v", len(grid), grid)
	}
	for i, row := range grid {
		got := cellsOf(row)
		if len(got) != 5 {
			t.Fatalf("row %d: want 5 cells, got %d: %q", i, len(got), got)
		}
	}
	if got := cellsOf(grid[0]); got[0] != "Defer work" || got[1] != "108" || got[4] != "33%" {
		t.Fatalf("row0 cells = %q", got)
	}
	if got := cellsOf(grid[1]); got[0] != "Deadlock avoidance" || got[1] != "35" {
		t.Fatalf("row1 cells = %q", got)
	}
	if got := cellsOf(grid[2]); got[0] != "General pumps" || got[1] != "48" {
		t.Fatalf("row2 cells = %q", got)
	}
	// The bold label look survives onto the first cell.
	if len(grid[0][0].runs) == 0 || !grid[0][0].runs[0].Look.Bold() {
		t.Fatalf("label lost its bold look")
	}
}

func TestIsNumericText(t *testing.T) {
	for _, s := range []string{"108", "31%", "0.05", "-3", "1,024"} {
		if !isNumericText(s) {
			t.Fatalf("%q should be numeric", s)
		}
	}
	for _, s := range []string{"Defer work", "Pumps", ""} {
		if isNumericText(s) {
			t.Fatalf("%q should not be numeric", s)
		}
	}
}

func TestLooksLikeTable(t *testing.T) {
	if !looksLikeTable(tioga.Block{Text: "a\tb\nc\td"}) {
		t.Fatal("two tabbed lines is a table")
	}
	if looksLikeTable(tioga.Block{Text: "a\tb"}) {
		t.Fatal("one tabbed line is not a table")
	}
	if looksLikeTable(tioga.Block{Text: "no tabs\nhere"}) {
		t.Fatal("no tabs is not a table")
	}
}
