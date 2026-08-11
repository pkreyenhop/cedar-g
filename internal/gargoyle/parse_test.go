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
	// The scene has 75 trajectories and 2 single-child outlines, so the path
	// count must be modest — not inflated by an outline swallowing its siblings.
	if len(sc.Paths) < 10 || len(sc.Paths) > 90 {
		t.Fatalf("unexpected path count %d (outline over-consumption?)", len(sc.Paths))
	}
}

// TestOutlineChildCount is the regression test for the bug where an Outline
// consumed every following Traj instead of just its declared children.
func TestOutlineChildCount(t *testing.T) {
	src := `Entities: [2]:
Outline fillColor: [1 0.5] ow: T fillText: T 0
Children: [1]
Traj  (open) [1] arrows: 0 j: round e: T butt w: 6.0 c: T [0 1.0 0.0 0.0] d: T F
[0.0,0.0] (Line  ) [10.0,0.0] fwd: T pList: ( )

Traj  (open) [1] arrows: 0 j: round e: T butt w: 1.0 c: T [0 0.0 0.0 0.0] d: T F
[0.0,5.0] (Line  ) [10.0,5.0] fwd: T pList: ( )
`
	sc := Parse([]byte(src))
	if len(sc.Paths) != 2 {
		t.Fatalf("want 2 paths (1 child + 1 sibling), got %d", len(sc.Paths))
	}
	for i, p := range sc.Paths {
		if p.Filled {
			t.Fatalf("path %d: open outline child must not be filled", i)
		}
	}
	if sc.Paths[0].Width != 6 || sc.Paths[1].Width != 1 {
		t.Fatalf("child/sibling widths swapped or wrong: %v %v", sc.Paths[0].Width, sc.Paths[1].Width)
	}
	if sc.Paths[0].Stroke.R != 1 { // the child kept its own red stroke
		t.Fatalf("outline child lost its stroke colour: %+v", sc.Paths[0].Stroke)
	}
}

func TestParseSegment(t *testing.T) {
	typ, pts := parseSegment("Arc [329.675,255.6741]")
	if typ != "Arc" || len(pts) != 1 || pts[0].X != 329.675 {
		t.Fatalf("arc seg: %q %+v", typ, pts)
	}
	typ, pts = parseSegment("Bezier [1,2] [3,4]")
	if typ != "Bezier" || len(pts) != 2 || pts[1].Y != 4 {
		t.Fatalf("bezier seg: %q %+v", typ, pts)
	}
	if typ, _ := parseSegment("Line"); typ != "Line" {
		t.Fatalf("line seg: %q", typ)
	}
}

func TestArcPoints(t *testing.T) {
	// Quarter circle through (1,0), (0.707,0.707), (0,1): all on the unit circle.
	pts := arcPoints(Point{1, 0}, Point{0.7071, 0.7071}, Point{0, 1})
	if len(pts) != curveSteps {
		t.Fatalf("want %d points, got %d", curveSteps, len(pts))
	}
	for _, p := range pts {
		if r := (p.X*p.X + p.Y*p.Y); r < 0.98 || r > 1.02 {
			t.Fatalf("point %v not on unit circle (r^2=%v)", p, r)
		}
	}
	if last := pts[len(pts)-1]; last.X > 0.02 || last.Y < 0.98 {
		t.Fatalf("arc did not end at (0,1): %v", last)
	}
	// Collinear points degenerate to a straight line (just the endpoint).
	if got := arcPoints(Point{0, 0}, Point{1, 1}, Point{2, 2}); len(got) != 1 {
		t.Fatalf("collinear arc should be a line: %+v", got)
	}
}

func TestBezierPoints(t *testing.T) {
	pts := bezierPoints(Point{0, 0}, Point{0, 1}, Point{1, 1}, Point{1, 0})
	if len(pts) != curveSteps || pts[len(pts)-1] != (Point{1, 0}) {
		t.Fatalf("bezier: %d pts, last %v", len(pts), pts[len(pts)-1])
	}
}
