package tioga

import (
	"math"
	"testing"
)

func closeRGB(a, b RGB) bool {
	f := func(x, y float32) bool { return math.Abs(float64(x-y)) < 0.01 }
	return f(a.R, b.R) && f(a.G, b.G) && f(a.B, b.B)
}

func TestHSVtoRGB(t *testing.T) {
	cases := []struct {
		h, s, v float64
		want    RGB
	}{
		{0, 1, 1, RGB{1, 0, 0}},         // red
		{0.667, 1, 1, RGB{0, 0, 1}},     // ~blue (240°)
		{1.0 / 3, 1, 1, RGB{0, 1, 0}},   // green (120°)
		{0, 0, 0.5, RGB{0.5, 0.5, 0.5}}, // grey
	}
	for _, c := range cases {
		if got := hsvToRGB(c.h, c.s, c.v); !closeRGB(got, c.want) {
			t.Fatalf("hsv(%v,%v,%v) = %+v, want %+v", c.h, c.s, c.v, got, c.want)
		}
	}
}

func TestTextColorOf(t *testing.T) {
	// The value framing includes binary bytes; the RPN phrase is "H S V textColor".
	props := []Prop{{Name: "CharProps", Value: "\x05\x01\aPostfix\x170.667 1.0 1.0 textColorn\x00"}}
	rgb, ok := TextColorOf(props)
	if !ok {
		t.Fatal("expected a colour")
	}
	if !closeRGB(rgb, RGB{0, 0, 1}) { // hue 0.667 -> blue
		t.Fatalf("colour = %+v, want blue", rgb)
	}
	// "0 1 1 textColor" is red (per the ButtonIdeas note).
	red := []Prop{{Name: "charprops", Value: "junk 0 1 1 textColor"}}
	if rgb, ok := TextColorOf(red); !ok || !closeRGB(rgb, RGB{1, 0, 0}) {
		t.Fatalf("red = %+v ok=%v", rgb, ok)
	}
	// No CharProps -> no colour.
	if _, ok := TextColorOf([]Prop{{Name: "Mark", Value: "x"}}); ok {
		t.Fatal("unexpected colour")
	}
}
