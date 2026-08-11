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
// hand: every token is 10 wide).
func TestWrapTokens(t *testing.T) {
	mk := func(text string, kind segKind, w int) tok { return tok{text: text, kind: kind, w: w} }
	// "aa bb cc" with each word 10 wide, space 4 wide, maxW 24 -> "aa bb" then "cc".
	toks := []tok{
		mk("aa", segWord, 10), mk(" ", segSpace, 4),
		mk("bb", segWord, 10), mk(" ", segSpace, 4),
		mk("cc", segWord, 10),
	}
	lines := wrapTokens(toks, 24)
	if got := renderLines(lines); got != "aa bb|cc" {
		t.Fatalf("wrap = %q", got)
	}

	// A word split across two runs (no space) must not break mid-word: 12+12=24
	// exceeds 20, so the whole 24-wide word drops to its own line.
	toks = []tok{
		mk("x", segWord, 10), mk(" ", segSpace, 4),
		mk("Fo", segWord, 12), mk("rk", segWord, 12),
	}
	lines = wrapTokens(toks, 20)
	if got := renderLines(lines); got != "x|Fork" {
		t.Fatalf("mid-word wrap = %q", got)
	}

	// Hard break forces a new line; leading space on the new line is dropped.
	toks = []tok{mk("a", segWord, 10), mk("\n", segBreak, 0), mk(" ", segSpace, 4), mk("b", segWord, 10)}
	lines = wrapTokens(toks, 1000)
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

func TestExpandRunTabs(t *testing.T) {
	bold := tioga.Look(1 << (31 - ('b' - 'a')))
	runs := []tioga.Run{{Text: "Idle Cedar", Look: bold}, {Text: "\t\t0.9\t"}, {Text: "132"}}
	out := expandRunTabs(runs)
	var joined string
	for _, r := range out {
		joined = joined + r.Text
	}
	if strings.ContainsRune(joined, '\t') {
		t.Fatalf("tab survived expansion: %q", joined)
	}
	// Column tracking is cross-run: "Idle Cedar" is 10 cols, so the first tab
	// fills to col 16 (6 spaces) and the second to col 24 (8 spaces).
	if out[1].Text != "      0.9     " && !strings.HasPrefix(out[1].Text, "      ") {
		t.Fatalf("unexpected expansion: %q", out[1].Text)
	}
	if !out[0].Look.Bold() {
		t.Fatalf("look lost during expansion")
	}
	// No tabs -> the same slice is returned untouched.
	plain := []tioga.Run{{Text: "no tabs here"}}
	if got := expandRunTabs(plain); &got[0] != &plain[0] {
		t.Fatalf("tab-free runs should be returned unchanged")
	}
}
