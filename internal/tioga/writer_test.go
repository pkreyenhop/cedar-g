package tioga

import "testing"

func TestEncodeRoundTrip(t *testing.T) {
	in := "Hello\nWorld\nThird line"
	doc := Read(Encode(in), false)
	if doc.IsCode {
		t.Fatalf("expected a document, got code")
	}
	// Three paragraphs means the container parsed (the plain-text fallback would
	// produce a single block with embedded newlines).
	want := []string{"Hello", "World", "Third line"}
	if len(doc.Blocks) != len(want) {
		t.Fatalf("got %d blocks, want %d: %+v", len(doc.Blocks), len(want), doc.Blocks)
	}
	for i, w := range want {
		if doc.Blocks[i].Kind != Paragraph || doc.Blocks[i].Text != w {
			t.Fatalf("block %d = %+v, want Paragraph %q", i, doc.Blocks[i], w)
		}
	}
}

func TestEncodeIsValidContainer(t *testing.T) {
	// init() must accept the encoded bytes (not fall back to plain text).
	r := &reader{}
	if !r.init(Encode("one\ntwo")) {
		t.Fatalf("encoded bytes rejected by init()")
	}
}

func TestEncodeArrowRoundTrip(t *testing.T) {
	doc := Read(Encode("a ← 0"), false)
	if len(doc.Blocks) != 1 || doc.Blocks[0].Text != "a ← 0" {
		t.Fatalf("arrow round-trip failed: %+v", doc.Blocks)
	}
}

func TestEncodeEmpty(t *testing.T) {
	// An empty buffer must still produce a valid container.
	r := &reader{}
	if !r.init(Encode("")) {
		t.Fatalf("empty encode rejected by init()")
	}
}
