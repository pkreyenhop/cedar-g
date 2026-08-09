package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTmp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func sumWeights(c *column) float32 {
	var s float32
	for _, w := range c.weights {
		s += w
	}
	return s
}

func approx1(w float32) bool { return w > 0.99 && w < 1.01 }

func TestOpenBalancesColumns(t *testing.T) {
	s := newUI()
	dir := t.TempDir()
	a := writeTmp(t, dir, "A.mesa", "A: CEDAR PROC = {}")
	b := writeTmp(t, dir, "B.tioga", "just a document")
	c := writeTmp(t, dir, "C.mesa", "C: CEDAR PROC = {}")
	s.setRoot(dir)

	s.openFile(a) // col0
	s.openFile(b) // col1 (shorter)
	s.openFile(c) // col0
	if len(s.cols[0].viewers) != 2 || len(s.cols[1].viewers) != 1 {
		t.Fatalf("columns = %d,%d, want 2,1", len(s.cols[0].viewers), len(s.cols[1].viewers))
	}
	if n := len(s.allViewers()); n != 3 {
		t.Fatalf("viewers = %d, want 3", n)
	}
	if !approx1(sumWeights(s.cols[0])) {
		t.Errorf("col0 weights sum = %v, want ~1", sumWeights(s.cols[0]))
	}
	// Reopening focuses; no new viewer.
	s.openFile(a)
	if n := len(s.allViewers()); n != 3 {
		t.Fatalf("after reopen viewers = %d, want 3", n)
	}
}

func TestMinimizeRestoreDestroy(t *testing.T) {
	s := newUI()
	dir := t.TempDir()
	a := writeTmp(t, dir, "A.mesa", "A: CEDAR PROC = {}")
	b := writeTmp(t, dir, "B.mesa", "B: CEDAR PROC = {}")
	s.setRoot(dir)
	s.openFile(a) // col0
	s.openFile(b) // col1
	va := s.cols[0].viewers[0]

	s.minimizeViewer(va)
	if len(s.minimized) != 1 || len(s.cols[0].viewers) != 0 {
		t.Fatalf("minimize: minimized=%d col0=%d", len(s.minimized), len(s.cols[0].viewers))
	}
	s.restoreViewer(va)
	if len(s.minimized) != 0 || len(s.cols[0].viewers) != 1 {
		t.Fatalf("restore: minimized=%d col0=%d", len(s.minimized), len(s.cols[0].viewers))
	}
	if !approx1(sumWeights(s.cols[0])) {
		t.Errorf("col0 weights sum = %v after restore", sumWeights(s.cols[0]))
	}
	s.destroyViewer(va)
	if n := len(s.allViewers()); n != 1 {
		t.Fatalf("after destroy viewers = %d, want 1", n)
	}
}

func TestSplitAndSwitch(t *testing.T) {
	s := newUI()
	dir := t.TempDir()
	a := writeTmp(t, dir, "A.mesa", "A: CEDAR PROC = {}")
	s.setRoot(dir)
	s.openFile(a)
	va := s.cols[0].viewers[0]

	s.splitViewer(va)
	if len(s.cols[0].viewers) != 2 {
		t.Fatalf("after split col0 = %d, want 2", len(s.cols[0].viewers))
	}
	if !approx1(sumWeights(s.cols[0])) {
		t.Errorf("col0 weights sum = %v after split", sumWeights(s.cols[0]))
	}
	s.switchViewer(va)
	if va.col != 1 || len(s.cols[1].viewers) != 1 {
		t.Fatalf("after switch: col=%d col1=%d", va.col, len(s.cols[1].viewers))
	}
}

func TestGrowToggle(t *testing.T) {
	s := newUI()
	dir := t.TempDir()
	a := writeTmp(t, dir, "A.mesa", "A: CEDAR PROC = {}")
	s.setRoot(dir)
	s.openFile(a)
	va := s.cols[0].viewers[0]

	s.growViewer(va)
	if s.cols[0].grown != va {
		t.Errorf("grow not set")
	}
	s.growViewer(va)
	if s.cols[0].grown != nil {
		t.Errorf("grow not cleared")
	}
}
