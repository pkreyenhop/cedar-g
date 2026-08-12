package mesa

import (
	"strings"
	"testing"
)

func TestSafetyAnalysis(t *testing.T) {
	// A plain arithmetic program is safe.
	safeSrc := `Foo: CEDAR PROGRAM IMPORTS IO = { IO.PutInt[6 * 7]; }.`
	m, _ := ParseSource(safeSrc)
	if s := AnalyzeSafety(safeSrc, m); !s.Safe() || s.Note() != "" {
		t.Fatalf("expected safe, got %+v", s)
	}

	// LOOPHOLE is an unsafe language feature.
	unsafeSrc := `Foo: CEDAR PROGRAM IMPORTS IO = {
  x: INTEGER ← LOOPHOLE[3.0, INTEGER];
}.`
	m, _ = ParseSource(unsafeSrc)
	s := AnalyzeSafety(unsafeSrc, m)
	if s.Safe() {
		t.Fatal("LOOPHOLE program should be flagged unsafe")
	}
	if !strings.Contains(s.Note(), "LOOPHOLE") {
		t.Fatalf("note missing LOOPHOLE: %q", s.Note())
	}

	// A low-level interface import is flagged.
	vmSrc := `Foo: CEDAR PROGRAM IMPORTS VM, IO = { IO.PutInt[1]; }.`
	m, _ = ParseSource(vmSrc)
	s = AnalyzeSafety(vmSrc, m)
	if s.Safe() || !strings.Contains(s.Note(), "VM") {
		t.Fatalf("expected VM flagged, got %+v", s)
	}

	// The interpreter still runs an unsafe program safely (LOOPHOLE == identity),
	// never escaping the host.
	if _, err := ParseSource(unsafeSrc); err != nil {
		t.Fatalf("unsafe program should still parse/run: %v", err)
	}
}
