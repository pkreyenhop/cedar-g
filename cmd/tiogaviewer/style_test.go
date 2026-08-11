package main

import (
	"testing"

	"gioui.org/font"
	"gioui.org/text"
)

func TestDocStyleFormats(t *testing.T) {
	body := docStyle("body", 1)
	if body.fnt.Typeface != "Serif" || body.fnt.Weight != font.Normal || body.size != docTextSize {
		t.Fatalf("body = %+v", body)
	}

	title := docStyle("title", 1)
	if title.fnt.Weight != font.Bold || title.align != text.Middle || title.size <= body.size {
		t.Fatalf("title should be big, bold, centred: %+v", title)
	}

	authors := docStyle("authors", 1)
	if authors.align != text.Middle {
		t.Fatalf("authors should be centred: %+v", authors)
	}

	// Headings are bold, and deeper heads are smaller than higher ones.
	h1, h2, h4 := docStyle("head", 2), docStyle("head2", 1), docStyle("head4", 1)
	if h1.fnt.Weight != font.Bold || h2.fnt.Weight != font.Bold {
		t.Fatalf("heads should be bold: %+v %+v", h1, h2)
	}
	if !(h1.size >= h2.size && h2.size >= h4.size) {
		t.Fatalf("head sizes should decrease: h1=%v h2=%v h4=%v", h1.size, h2.size, h4.size)
	}

	code := docStyle("code", 1)
	if code.fnt.Typeface != "Mono" {
		t.Fatalf("code should be monospace: %+v", code)
	}

	center := docStyle("center", 1)
	if center.align != text.Middle {
		t.Fatalf("center should be centred: %+v", center)
	}
}

func TestDocStyleIndent(t *testing.T) {
	// Format indent: block/item/item1 increase.
	if docStyle("body", 1).indent != 0 {
		t.Fatalf("body should not indent")
	}
	if !(docStyle("item", 1).indent > 0 && docStyle("item1", 1).indent > docStyle("item", 1).indent) {
		t.Fatalf("item indents should grow")
	}
	// Nesting indent: deeper nodes indent more, independent of format.
	shallow := docStyle("body", 1).indent
	deep := docStyle("body", 4).indent
	if !(deep > shallow) {
		t.Fatalf("nesting should add indent: depth1=%v depth4=%v", shallow, deep)
	}
	// Unknown format falls back to body.
	if docStyle("nonesuch", 1) != docStyle("body", 1) {
		t.Fatalf("unknown format should fall back to body")
	}
}

func TestHeadLevelAndSize(t *testing.T) {
	if headLevel("head3", 1) != 3 {
		t.Fatalf("head3 level")
	}
	if headLevel("head", 4) != 3 { // bare head takes level from depth-1
		t.Fatalf("bare head level from depth")
	}
	if !isHeadFormat("head") || !isHeadFormat("head2") || isHeadFormat("header") || isHeadFormat("head10") {
		t.Fatalf("isHeadFormat mismatch")
	}
	if !(headSize(1) > headSize(2) && headSize(2) > headSize(3)) {
		t.Fatalf("head sizes not monotonic")
	}
}
