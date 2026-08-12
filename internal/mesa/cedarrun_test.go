package mesa

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"cedarg/internal/tioga"
)

// TestCedarRunRate measures how many real Cedar sources elaborate — parse, then
// execute their top-level declarations (types, constants, procedure bindings) —
// without a runtime error. Most modules are libraries with no main body, so this
// tracks how far the interpreter gets binding a module before hitting an
// unsupported construct. Set CEDAR_DIR to a directory of .mesa files.
func TestCedarRunRate(t *testing.T) {
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

	ok, parseFail, runFail := 0, 0, 0
	errs := map[string]int{}
	var samples []string
	for _, f := range files {
		data, _ := os.ReadFile(f)
		m, err := ParseSource(tioga.Read(data, true).Code)
		if err != nil {
			parseFail++
			continue
		}
		var buf bytes.Buffer
		err = runGuarded(&buf, m)
		if err != nil {
			runFail++
			errs[classify(err.Error())]++
			if len(samples) < 15 {
				samples = append(samples, filepath.Base(f)+": "+err.Error())
			}
			continue
		}
		ok++
	}
	tot := ok + parseFail + runFail
	t.Logf("elaborated %d/%d (%.0f%%);  parse-fail %d;  run-fail %d",
		ok, tot, 100*float64(ok)/float64(tot+1), parseFail, runFail)
	type kv struct {
		k string
		n int
	}
	var top []kv
	for k, n := range errs {
		top = append(top, kv{k, n})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n })
	for i, e := range top {
		if i >= 15 {
			break
		}
		t.Logf("  %4d  %s", e.n, e.k)
	}
	for _, s := range samples {
		if len(s) > 110 {
			s = s[:110]
		}
		t.Logf("  eg: %s", s)
	}
}

// runGuarded runs a module, converting any panic into an error so one bad
// module cannot abort the survey. (Run already recovers runtimeError; this
// catches anything else too.)
func runGuarded(out *bytes.Buffer, m *Module) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	in := NewInterp(out)
	in.SetMaxSteps(2_000_000) // tight per-module bound for a bulk survey
	return in.Run(m)
}
