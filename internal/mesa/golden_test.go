package mesa

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"cedarg/internal/tioga"
)

// TestGoldens runs every curated sample program and checks its output against a
// golden file in samples/golden/<name>.out. It is the north-star harness for the
// safe runnable subset: the pass count is the metric to grow. Run with
// GEN_GOLDEN=1 to (re)generate the golden files after eyeballing the output.
func TestGoldens(t *testing.T) {
	files, _ := filepath.Glob("samples/*.mesa")
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no samples found")
	}
	gen := os.Getenv("GEN_GOLDEN") != ""
	pass := 0
	for _, f := range files {
		base := filepath.Base(f)
		t.Run(base, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			// .mesa samples are plain text here, but decode through the Tioga code
			// reader anyway so the harness matches how real files are run.
			src := tioga.Read(data, true).Code
			m, err := ParseSource(src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			var buf bytes.Buffer
			if err := NewInterp(&buf).Run(m); err != nil {
				t.Fatalf("run: %v\noutput so far:\n%s", err, buf.String())
			}
			goldenPath := filepath.Join("samples", "golden", base+".out")
			if gen {
				if err := os.WriteFile(goldenPath, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s", goldenPath)
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Skipf("no golden (%s); run with GEN_GOLDEN=1 to create it", goldenPath)
				return
			}
			if buf.String() != string(want) {
				t.Fatalf("output mismatch:\n--- got ---\n%s\n--- want ---\n%s", buf.String(), string(want))
			}
			pass++
		})
	}
	t.Logf("golden pass: %d/%d samples", pass, len(files))
}
