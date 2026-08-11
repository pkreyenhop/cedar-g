package gargoyle

import (
	"os"
	"testing"
)

func load(t *testing.T, name string) *Scene {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Skipf("missing testdata: %v", err)
	}
	return Parse(data)
}

func TestParseScanner(t *testing.T) {
	toks := scan([]byte(`Text T "hi there" font [1 2 3] (Line  ) foo`))
	kinds := []tkind{tWord, tWord, tStr, tWord, tBrack, tParen, tWord}
	if len(toks) != len(kinds) {
		t.Fatalf("got %d tokens: %+v", len(toks), toks)
	}
	if toks[2].s != "hi there" || toks[4].s != "1 2 3" || toks[5].s != "Line" {
		t.Fatalf("token contents wrong: %+v", toks)
	}
}

func TestParseColor(t *testing.T) {
	if c := parseColor("0 0.0 0.0 0.8"); c.B != 0.8 || c.R != 0 {
		t.Fatalf("rgb = %+v", c)
	}
	// gray 1.0 is full ink -> black; gray 0.0 -> white.
	if c := parseColor("1 1.0"); c.R != 0 {
		t.Fatalf("gray 1.0 should be black: %+v", c)
	}
	if c := parseColor("1 0.0"); c.R != 1 {
		t.Fatalf("gray 0.0 should be white: %+v", c)
	}
}

func TestParseFig3(t *testing.T) {
	sc := load(t, "fig3.gargoyle")
	if sc.Empty() {
		t.Fatal("scene is empty")
	}
	if len(sc.Paths) < 20 {
		t.Fatalf("expected many paths, got %d", len(sc.Paths))
	}
	// The five labels should be present, including "Figure 3".
	want := map[string]bool{
		"Low priority notifying thread": false,
		"High priority waiting thread":  false,
		"Naive wakeup":                  false,
		"Careful wakeup":                false,
		"Figure 3":                      false,
	}
	for _, l := range sc.Labels {
		if _, ok := want[l.Text]; ok {
			want[l.Text] = true
		}
	}
	for txt, found := range want {
		if !found {
			t.Fatalf("label %q not parsed (labels=%d)", txt, len(sc.Labels))
		}
	}
	// A non-degenerate bounding box.
	if sc.Width() <= 0 || sc.Height() <= 0 {
		t.Fatalf("bad bounds: %+v .. %+v", sc.Min, sc.Max)
	}
	// Every path has at least two points and a positive width.
	for i, p := range sc.Paths {
		if len(p.Pts) < 1 {
			t.Fatalf("path %d has no points", i)
		}
		if p.Width <= 0 {
			t.Fatalf("path %d width %v", i, p.Width)
		}
	}
}

func TestParseFig12(t *testing.T) {
	sc := load(t, "fig12.gargoyle")
	if len(sc.Paths) < 10 {
		t.Fatalf("expected paths, got %d", len(sc.Paths))
	}
	// This scene has filled outlines.
	filled := 0
	for _, p := range sc.Paths {
		if p.Filled {
			filled++
		}
	}
	if filled == 0 {
		t.Fatalf("expected some filled outlines")
	}
}
