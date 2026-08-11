package main

import (
	"strings"
	"testing"

	"cedarg/internal/tioga"
)

func TestSplitSegments(t *testing.T) {
	segs := splitSegments("a  bc\nd")
	want := []segment{
		{"a", segWord}, {"  ", segSpace}, {"bc", segWord},
		{"\n", segBreak}, {"d", segWord},
	}
	if len(segs) != len(want) {
		t.Fatalf("segments = %+v", segs)
	}
	for i := range want {
		if segs[i] != want[i] {
			t.Fatalf("seg %d = %+v, want %+v", i, segs[i], want[i])
		}
	}
}

// TestWrapTokens checks greedy wrapping, mid-word protection, leading-space
// dropping and hard breaks — all independent of font shaping (widths are set by
// hand). tabStop 0 disables tab advancing here.
func TestWrapTokens(t *testing.T) {
	mk := func(text string, kind segKind, w int) tok { return tok{text: text, kind: kind, w: w} }
	// "aa bb cc" with each word 10 wide, space 4 wide, maxW 24 -> "aa bb" then "cc".
	toks := []tok{
		mk("aa", segWord, 10), mk(" ", segSpace, 4),
		mk("bb", segWord, 10), mk(" ", segSpace, 4),
		mk("cc", segWord, 10),
	}
	lines := wrapTokens(toks, 24, 0)
	if got := renderLines(lines); got != "aa bb|cc" {
		t.Fatalf("wrap = %q", got)
	}

	// A word split across two runs (no space) must not break mid-word: 12+12=24
	// exceeds 20, so the whole 24-wide word drops to its own line.
	toks = []tok{
		mk("x", segWord, 10), mk(" ", segSpace, 4),
		mk("Fo", segWord, 12), mk("rk", segWord, 12),
	}
	lines = wrapTokens(toks, 20, 0)
	if got := renderLines(lines); got != "x|Fork" {
		t.Fatalf("mid-word wrap = %q", got)
	}

	// Hard break forces a new line; leading space on the new line is dropped.
	toks = []tok{mk("a", segWord, 10), mk("\n", segBreak, 0), mk(" ", segSpace, 4), mk("b", segWord, 10)}
	lines = wrapTokens(toks, 1000, 0)
	if got := renderLines(lines); got != "a|b" {
		t.Fatalf("hard-break wrap = %q", got)
	}
}

func TestHasLooks(t *testing.T) {
	bold := tioga.Look(1 << (31 - ('b' - 'a')))
	if hasLooks([]tioga.Run{{Text: "a"}, {Text: "b"}}) {
		t.Fatal("plain runs reported looks")
	}
	if !hasLooks([]tioga.Run{{Text: "a"}, {Text: "b", Look: bold}}) {
		t.Fatal("bold run not detected")
	}
}

func renderLines(lines [][]tok) string {
	var out []string
	for _, line := range lines {
		var sb strings.Builder
		for _, t := range line {
			sb.WriteString(t.text)
		}
		out = append(out, sb.String())
	}
	return strings.Join(out, "|")
}

func TestTabStops(t *testing.T) {
	// Two rows with different-width labels must land their value column at the
	// same x: a tab advances to the next multiple of tabStop from the line start.
	const stop = 100
	valX := func(labelW int) int {
		toks := []tok{{kind: segWord, w: labelW}, {kind: segTab}, {kind: segWord, w: 20}}
		lines := wrapTokens(toks, 10000, stop)
		x := 0
		for _, tk := range lines[0] {
			if tk.kind == segWord && x > 0 {
				return x
			}
			x += tk.w
		}
		return -1
	}
	if a, b := valX(30), valX(85); a != 100 || b != 100 || a != b {
		t.Fatalf("value column misaligned: short=%d wide=%d", a, b)
	}
	if got := valX(140); got != 200 {
		t.Fatalf("label past a stop should reach the next: got %d", got)
	}
}

func TestSplitSegmentsTabs(t *testing.T) {
	segs := splitSegments("Idle	Cedar		22")
	tabs, words := 0, 0
	for _, seg := range segs {
		switch seg.kind {
		case segTab:
			tabs++
		case segWord:
			words++
		}
	}
	if tabs != 3 || words != 3 {
		t.Fatalf("tabs=%d words=%d for %q", tabs, words, "Idle	Cedar		22")
	}
}
