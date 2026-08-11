// Package gargoyle parses the Cedar "Gargoyle" 2D scene format — the editable
// vector artwork embedded in Tioga documents as a node's GGFile property. It
// produces a small, toolkit-neutral scene model (polylines, filled outlines and
// text labels) that a GUI can draw directly, so the figures in a Cedar paper can
// be rendered instead of shown as a placeholder.
//
// The format is a line-oriented text format: a header of editor settings, then
// "Entities: [N]:" followed by entities. Only three entity kinds carry drawable
// content — Traj (a trajectory: a polyline of Line segments with a stroke width
// and colour), Outline (a filled region whose Children are boundary Trajs), and
// Text (a label placed by an affine transform). Trailing Class/Menu/… sections
// are editor metadata and are ignored.
package gargoyle

import (
	"math"
	"strconv"
	"strings"
)

// Color is a straight RGBA colour in 0..1.
type Color struct{ R, G, B, A float32 }

// Point is a scene-space coordinate (Y increases upward, as Gargoyle stores it).
type Point struct{ X, Y float64 }

// Path is a polyline: a stroked open/closed trajectory, or a filled outline.
type Path struct {
	Pts    []Point
	Width  float64
	Stroke Color
	Closed bool
	Filled bool
	Fill   Color
}

// Label is a text string placed at a baseline origin with a point size.
type Label struct {
	Text   string
	X, Y   float64
	Size   float64
	Color  Color
	Italic bool
}

// Scene is a parsed Gargoyle drawing plus the bounding box of its content.
type Scene struct {
	Paths    []Path
	Labels   []Label
	Min, Max Point
}

// Empty reports whether the scene has nothing to draw.
func (s *Scene) Empty() bool { return len(s.Paths) == 0 && len(s.Labels) == 0 }

// Width and Height are the content bounding-box extents.
func (s *Scene) Width() float64  { return s.Max.X - s.Min.X }
func (s *Scene) Height() float64 { return s.Max.Y - s.Min.Y }

// Parse decodes a Gargoyle scene. It never errors on unknown constructs — it
// extracts what it understands and stops at the first non-entity section.
func Parse(data []byte) *Scene {
	toks := scan(data)
	p := &parser{toks: toks}
	return p.parse()
}

// ---- scanner ----

type tkind int

const (
	tWord  tkind = iota
	tStr         // "quoted"
	tBrack       // [ ... ]
	tParen       // ( ... )
)

type token struct {
	kind tkind
	s    string // inner text (delimiters stripped)
}

func scan(data []byte) []token {
	var toks []token
	s := string(data)
	i, n := 0, len(s)
	isSpace := func(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }
	for i < n {
		c := s[i]
		switch {
		case isSpace(c):
			i++
		case c == '"':
			j := i + 1
			for j < n && s[j] != '"' {
				j++
			}
			toks = append(toks, token{tStr, s[i+1 : j]})
			i = j + 1
		case c == '[':
			j := i + 1
			for j < n && s[j] != ']' {
				j++
			}
			toks = append(toks, token{tBrack, s[i+1 : j]})
			i = j + 1
		case c == '(':
			j := i + 1
			for j < n && s[j] != ')' {
				j++
			}
			toks = append(toks, token{tParen, strings.TrimSpace(s[i+1 : j])})
			i = j + 1
		default:
			j := i
			for j < n && !isSpace(s[j]) && s[j] != '[' && s[j] != '(' && s[j] != '"' {
				j++
			}
			toks = append(toks, token{tWord, s[i:j]})
			i = j
		}
	}
	return toks
}

// ---- parser ----

type parser struct {
	toks []token
	i    int
}

func (p *parser) parse() *Scene {
	sc := &Scene{}
	// Skip the header up to the entities section.
	for p.i < len(p.toks) && !(p.toks[p.i].kind == tWord && p.toks[p.i].s == "Entities:") {
		p.i++
	}
	p.i++ // past "Entities:"
	// Skip the "[N]" count and the ":" that follow.
	for p.i < len(p.toks) && (p.toks[p.i].kind == tBrack || (p.toks[p.i].kind == tWord && p.toks[p.i].s == ":")) {
		p.i++
	}

	for p.i < len(p.toks) {
		t := p.toks[p.i]
		if t.kind != tWord {
			p.i++
			continue
		}
		switch t.s {
		case "Traj":
			if path, ok := p.parseTraj(); ok {
				sc.Paths = append(sc.Paths, path)
			}
		case "Outline":
			p.parseOutline(sc)
		case "Text":
			if lbl, ok := p.parseText(); ok {
				sc.Labels = append(sc.Labels, lbl)
			}
		case "Class", "Menu", "MessageHandler", "Feedback", "Caret":
			p.i = len(p.toks) // editor metadata: nothing drawable follows
		default:
			p.i++
		}
	}
	sc.computeBounds()
	return sc
}

func isEntityStart(t token) bool {
	if t.kind != tWord {
		return false
	}
	switch t.s {
	case "Traj", "Outline", "Text", "Class", "Menu", "MessageHandler", "Feedback", "Caret", "Children:":
		return true
	}
	return false
}

// parseTraj reads a trajectory starting at the current "Traj" token, capturing
// its stroke width, colour, open/closed flag and points.
func (p *parser) parseTraj() (Path, bool) {
	p.i++ // past "Traj"
	path := Path{Width: 1, Stroke: Color{0, 0, 0, 1}}
	// Attributes, up to the first point (a bracket containing a comma).
	for p.i < len(p.toks) {
		t := p.toks[p.i]
		if t.kind == tBrack && strings.Contains(t.s, ",") {
			break
		}
		if t.kind == tParen {
			if strings.Contains(t.s, "closed") {
				path.Closed = true
			}
			p.i++
			continue
		}
		if t.kind == tWord {
			switch t.s {
			case "w:":
				if p.i+1 < len(p.toks) {
					path.Width = atof(p.toks[p.i+1].s)
				}
			case "c:":
				// c: <T|F> [colour]
				p.i++
				for p.i < len(p.toks) && p.toks[p.i].kind != tBrack {
					p.i++
				}
				if p.i < len(p.toks) {
					path.Stroke = parseColor(p.toks[p.i].s)
				}
			}
		}
		p.i++
	}
	// Points: [x,y] segments, separated by (Line) tokens, until "fwd:".
	for p.i < len(p.toks) {
		t := p.toks[p.i]
		if t.kind == tBrack && strings.Contains(t.s, ",") {
			path.Pts = append(path.Pts, parsePoint(t.s))
			p.i++
			continue
		}
		if t.kind == tParen { // "Line" segment separator
			p.i++
			continue
		}
		if isEntityStart(t) || (t.kind == tWord && t.s == "fwd:") {
			break
		}
		p.i++
	}
	p.skipToEntity()
	return path, len(path.Pts) > 0
}

// parseOutline reads an Outline and exactly its declared number of child
// trajectories. A child is filled with the outline's fillColor only when it is a
// closed polygon; open children (e.g. the thick "has-CPU" bars) keep their own
// stroke. Honouring the Children count is essential — the trajectories that
// follow the outline in the stream are siblings, not children.
func (p *parser) parseOutline(sc *Scene) {
	p.i++ // past "Outline"
	fill := Color{0.5, 0.5, 0.5, 1}
	n := 0
	for p.i < len(p.toks) {
		t := p.toks[p.i]
		if t.kind == tWord && t.s == "fillColor:" {
			p.i++
			for p.i < len(p.toks) && p.toks[p.i].kind != tBrack {
				p.i++
			}
			if p.i < len(p.toks) {
				fill = parseColor(p.toks[p.i].s)
			}
			p.i++
			continue
		}
		if t.kind == tWord && t.s == "Children:" {
			p.i++
			if p.i < len(p.toks) && p.toks[p.i].kind == tBrack {
				n = int(atof(p.toks[p.i].s)) // the [N] child count
				p.i++
			}
			break
		}
		if isEntityStart(t) {
			return // malformed: no children section
		}
		p.i++
	}
	for k := 0; k < n && p.i < len(p.toks) && p.toks[p.i].kind == tWord && p.toks[p.i].s == "Traj"; k++ {
		path, ok := p.parseTraj()
		if !ok {
			continue
		}
		if path.Closed && len(path.Pts) >= 3 {
			path.Filled = true
			path.Fill = fill
		}
		sc.Paths = append(sc.Paths, path)
	}
}

// parseText reads a Text label. Its placement transform is [a b c d e f] with
// origin (c, f) and size from the x-scale a.
func (p *parser) parseText() (Label, bool) {
	p.i++ // past "Text"
	var lbl Label
	lbl.Color = Color{0, 0, 0, 1}
	// The quoted string is the text.
	for p.i < len(p.toks) && p.toks[p.i].kind != tStr {
		if isEntityStart(p.toks[p.i]) {
			return lbl, false
		}
		p.i++
	}
	if p.i >= len(p.toks) {
		return lbl, false
	}
	lbl.Text = p.toks[p.i].s
	p.i++
	// The font path (word) precedes the transform; italic fonts end in "-I".
	if p.i < len(p.toks) && p.toks[p.i].kind == tWord {
		f := strings.ToLower(p.toks[p.i].s)
		lbl.Italic = strings.HasSuffix(f, "-i") || strings.Contains(f, "italic")
		p.i++
	}
	// Placement transform.
	for p.i < len(p.toks) && p.toks[p.i].kind != tBrack {
		p.i++
	}
	if p.i < len(p.toks) {
		tr := parseNums(p.toks[p.i].s)
		if len(tr) >= 6 {
			lbl.X, lbl.Y = tr[2], tr[5]
			lbl.Size = math.Hypot(tr[0], tr[1])
		}
		p.i++
	}
	// Colour.
	for p.i < len(p.toks) && p.toks[p.i].kind != tBrack {
		if isEntityStart(p.toks[p.i]) {
			break
		}
		p.i++
	}
	if p.i < len(p.toks) && p.toks[p.i].kind == tBrack {
		lbl.Color = parseColor(p.toks[p.i].s)
		p.i++
	}
	p.skipToEntity()
	return lbl, lbl.Text != "" && lbl.Size > 0
}

// skipToEntity advances to the next entity-start token.
func (p *parser) skipToEntity() {
	for p.i < len(p.toks) && !isEntityStart(p.toks[p.i]) {
		p.i++
	}
}

func (s *Scene) computeBounds() {
	first := true
	upd := func(x, y float64) {
		if first {
			s.Min = Point{x, y}
			s.Max = Point{x, y}
			first = false
			return
		}
		s.Min.X = math.Min(s.Min.X, x)
		s.Min.Y = math.Min(s.Min.Y, y)
		s.Max.X = math.Max(s.Max.X, x)
		s.Max.Y = math.Max(s.Max.Y, y)
	}
	for _, pa := range s.Paths {
		for _, pt := range pa.Pts {
			upd(pt.X, pt.Y)
		}
	}
	for _, l := range s.Labels {
		upd(l.X, l.Y)
		// Approximate the text box so labels are not clipped by the bounds.
		upd(l.X+float64(len([]rune(l.Text)))*l.Size*0.55, l.Y+l.Size)
	}
}

// ---- value parsing ----

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func parseNums(s string) []float64 {
	fs := strings.Fields(s)
	out := make([]float64, 0, len(fs))
	for _, f := range fs {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func parsePoint(s string) Point {
	i := strings.IndexByte(s, ',')
	if i < 0 {
		return Point{}
	}
	return Point{atof(s[:i]), atof(s[i+1:])}
}

// parseColor decodes Gargoyle colour groups: "0 r g b" is straight RGB (0=black);
// "1 g" is grayscale specified as ink amount (0=white, 1=black), so luminance is
// 1-g. Anything else is opaque black.
func parseColor(s string) Color {
	f := parseNums(s)
	if len(f) == 0 {
		return Color{0, 0, 0, 1}
	}
	switch int(f[0]) {
	case 0:
		if len(f) >= 4 {
			return Color{float32(f[1]), float32(f[2]), float32(f[3]), 1}
		}
	case 1:
		if len(f) >= 2 {
			v := float32(1 - f[1])
			return Color{v, v, v, 1}
		}
	}
	return Color{0, 0, 0, 1}
}
