package tioga

import (
	"math"
	"strconv"
	"strings"
)

// RGB is a colour in straight 0..1 components, decoded from a node's postfix
// properties.
type RGB struct{ R, G, B float32 }

// TextColorOf returns the text colour a node's CharProps sets, or (_, false).
// Character properties are stored as a serialised "Postfix" program; the colour
// is written as "H S V textColor", where the three operands are hue/saturation/
// value (Cedar's colour model — e.g. "0 1 1 textColor" is red).
func TextColorOf(props []Prop) (RGB, bool) {
	for _, p := range props {
		if !strings.EqualFold(p.Name, "CharProps") {
			continue
		}
		if rgb, ok := parseTextColor(p.Value); ok {
			return rgb, true
		}
	}
	return RGB{}, false
}

// parseTextColor finds a "… H S V textColor" phrase in a (partly binary)
// property value and returns the resolved RGB colour.
func parseTextColor(raw string) (RGB, bool) {
	s := sanitize(raw)
	i := strings.Index(s, "textColor")
	if i < 0 {
		return RGB{}, false
	}
	var nums []float64
	for _, f := range strings.Fields(s[:i]) {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			nums = append(nums, v)
		} else {
			nums = nil // a non-number resets the run, so we keep the last three
		}
	}
	if len(nums) < 3 {
		return RGB{}, false
	}
	n := len(nums)
	return hsvToRGB(nums[n-3], nums[n-2], nums[n-1]), true
}

// sanitize keeps the printable content of a property value (digits, letters and
// the numeric punctuation), turning binary framing bytes into spaces so the RPN
// tokens can be read.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r == '.', r == '-', r == '+':
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// hsvToRGB converts hue/saturation/value (all 0..1, hue wrapping at 1) to RGB.
func hsvToRGB(h, s, v float64) RGB {
	h = h - math.Floor(h) // wrap hue into [0,1)
	if s <= 0 {
		return RGB{float32(v), float32(v), float32(v)}
	}
	h *= 6
	i := math.Floor(h)
	f := h - i
	p := v * (1 - s)
	q := v * (1 - s*f)
	t := v * (1 - s*(1-f))
	var r, g, b float64
	switch int(i) % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return RGB{float32(r), float32(g), float32(b)}
}
