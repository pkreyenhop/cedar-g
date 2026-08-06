package mirror

import "testing"

func TestMatchExt(t *testing.T) {
	exts := []string{"mesa", "tioga"}
	cases := map[string]bool{
		"/a/b/Foo.mesa":       true,
		"/a/b/Doc.tioga":      true,
		"/a/b/Foo.mesa!3":     true, // versioned variant
		"/a/b/Doc.tioga!12":   true,
		"/a/b/.Foo.mesa.html": false, // generated view
		"/a/b/Foo.mesa.txt":   false,
		"/a/b/README":         false,
		"/a/b/Makefile":       false,
		"/a/b/thing.mesaish":  false, // suffix must be exact
		"/a/b/x.c":            false,
	}
	for p, want := range cases {
		if got := matchExt(p, exts); got != want {
			t.Errorf("matchExt(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestIsGeneratedView(t *testing.T) {
	// Note: ".index.html" also looks like a generated view, but callers test
	// isIndexPage first, so it never reaches isGeneratedView.
	yes := []string{"/a/.Foo.mesa.html", "/a/.README~.txt", "/a/.x.html"}
	no := []string{"/a/Foo.mesa", "/a/index.html", "/a/README"}
	for _, p := range yes {
		if !isGeneratedView(p) {
			t.Errorf("isGeneratedView(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if isGeneratedView(p) {
			t.Errorf("isGeneratedView(%q) = true, want false", p)
		}
	}
}

func TestIsIndexPage(t *testing.T) {
	if !isIndexPage("/a/b/.index.html") {
		t.Error("expected .index.html to be an index page")
	}
	if isIndexPage("/a/b/.Foo.mesa.html") {
		t.Error("did not expect a view page to be an index page")
	}
}
