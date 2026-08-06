package tioga

import "testing"

func TestNonTiogaCodeFallback(t *testing.T) {
	doc := Read([]byte("hello\nworld"), true)
	if !doc.IsCode || doc.Code != "hello\nworld" {
		t.Fatalf("code fallback: %+v", doc)
	}
}

func TestNonTiogaDocFallback(t *testing.T) {
	doc := Read([]byte("just text"), false)
	if doc.IsCode || len(doc.Blocks) != 1 || doc.Blocks[0].Text != "just text" {
		t.Fatalf("doc fallback: %+v", doc)
	}
}

func TestToStringGlyphs(t *testing.T) {
	// 0xD3 -> '©', 0xAC and '_' -> '←'.
	got := toString([]byte{'a', 0xd3, 0xac, '_', 'b'})
	want := "a©←←b"
	if got != want {
		t.Fatalf("toString = %q, want %q", got, want)
	}
}

func TestShortBufferIsNotTioga(t *testing.T) {
	// Too short to contain a trailer: must fall back, not panic.
	doc := Read([]byte{1, 2, 3}, false)
	if doc.IsCode {
		t.Fatalf("unexpected code doc")
	}
}
