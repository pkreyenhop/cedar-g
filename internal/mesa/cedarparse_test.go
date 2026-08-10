package mesa

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"cedarg/internal/tioga"
)

// TestCedarParseRate measures how many real Cedar sources parse. Set CEDAR_DIR
// to a directory of .mesa files; without it the test is skipped.
func TestCedarParseRate(t *testing.T) {
	dir := os.Getenv("CEDAR_DIR")
	if dir == "" {
		t.Skip("set CEDAR_DIR")
	}
	var files []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(p) == ".mesa" {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	clean, recov, fail := 0, 0, 0
	errs := map[string]int{}
	var samples []string
	for _, f := range files {
		data, _ := os.ReadFile(f)
		doc := tioga.Read(data, true)
		m, err := ParseSource(doc.Code)
		switch {
		case err != nil:
			fail++
			errs[classify(err.Error())]++
			if len(samples) < 12 {
				samples = append(samples, filepath.Base(f)+": "+err.Error())
			}
		case m.Recovered == 0:
			clean++
		default:
			recov++
		}
	}
	tot := clean + recov + fail
	t.Logf("clean %d/%d (%.0f%%);  parsed incl. recovery %d/%d (%.0f%%);  hard-fail %d",
		clean, tot, 100*float64(clean)/float64(tot+1),
		clean+recov, tot, 100*float64(clean+recov)/float64(tot+1), fail)
	type kv struct{ k string; n int }
	var top []kv
	for k, n := range errs {
		top = append(top, kv{k, n})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n })
	for i, e := range top {
		if i >= 12 { break }
		t.Logf("  %4d  %s", e.n, e.k)
	}
	for _, s := range samples {
		t.Logf("  eg: %s", s)
	}
}

// classify collapses an error to its reason (after the position/near clause).
func classify(s string) string {
	if i := strings.LastIndex(s, "): "); i >= 0 {
		return s[i+3:]
	}
	if i := strings.Index(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return s
}
