package main

import (
	"testing"

	"cedarg/internal/tioga"
)

func mkDoc(depths ...int) *viewer {
	v := &viewer{kind: vkContent}
	for _, d := range depths {
		v.blocks = append(v.blocks, tioga.Block{Text: "x", Depth: d})
	}
	return v
}

func TestVisibleBlocksLevelCap(t *testing.T) {
	v := mkDoc(1, 2, 3, 2, 1)
	if len(v.visibleBlocks()) != 5 {
		t.Fatalf("cap 0 should show all")
	}
	v.levelCap = 1
	if got := len(v.visibleBlocks()); got != 2 {
		t.Fatalf("cap 1 -> %d, want 2", got)
	}
	v.levelCap = 2
	if got := len(v.visibleBlocks()); got != 4 {
		t.Fatalf("cap 2 -> %d, want 4", got)
	}
}

func TestMaxBlockDepth(t *testing.T) {
	if d := mkDoc(1, 3, 2).maxBlockDepth(); d != 3 {
		t.Fatalf("maxBlockDepth = %d", d)
	}
	if d := (&viewer{}).maxBlockDepth(); d != 1 {
		t.Fatalf("empty maxBlockDepth = %d, want 1", d)
	}
}

func TestIsDoc(t *testing.T) {
	if !mkDoc(1).isDoc() {
		t.Fatal("prose doc should be isDoc")
	}
	if (&viewer{kind: vkContent, isCode: true}).isDoc() {
		t.Fatal("code viewer is not a doc")
	}
	if (&viewer{kind: vkTerm}).isDoc() {
		t.Fatal("terminal is not a doc")
	}
}
