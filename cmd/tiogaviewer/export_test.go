package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cedarg/internal/tioga"
)

func TestExportHTML(t *testing.T) {
	bold := tioga.Look(1 << (31 - ('b' - 'a')))
	blocks := []tioga.Block{
		{Format: "head", Depth: 2, Text: "Intro", Kind: tioga.Heading, Level: 1},
		{Format: "body", Text: "hello world", Runs: []tioga.Run{{Text: "hello "}, {Text: "world", Look: bold}}},
		{Format: "table3", Text: "A\t1\nB\t2", Runs: []tioga.Run{{Text: "A\t1\nB\t2"}}},
	}
	out := exportHTML("Doc", blocks)
	for _, want := range []string{"<h1>Intro</h1>", "<b>world</b>", "<table>", "<td", "<!doctype html>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("export missing %q:\n%s", want, out)
		}
	}
}

func TestComputeRefs(t *testing.T) {
	v := &viewer{blocks: []tioga.Block{
		{Text: "See TiogaDoc.tioga and AISIO.mesa for details."},
		{Text: "Also Graphics3d-Suite.df and TiogaDoc.tioga again."},
	}}
	v.computeRefs()
	got := map[string]bool{}
	for _, r := range v.refs {
		got[r.name] = true
	}
	for _, want := range []string{"TiogaDoc.tioga", "AISIO.mesa", "Graphics3d-Suite.df"} {
		if !got[want] {
			t.Fatalf("ref %q not found in %v", want, got)
		}
	}
	if len(v.refs) != 3 { // unique
		t.Fatalf("expected 3 unique refs, got %d", len(v.refs))
	}
}

func TestResolveRefVersioned(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Foo.tioga!3"), []byte("x"), 0o644)
	s := newUI()
	s.setRoot(dir)
	v := &viewer{path: filepath.Join(dir, "doc.tioga")}
	p, ok := s.resolveRef(v, "Foo.tioga")
	if !ok || filepath.Base(p) != "Foo.tioga!3" {
		t.Fatalf("versioned resolve = %q ok=%v", p, ok)
	}
}
